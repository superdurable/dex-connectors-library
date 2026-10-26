// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package openai implements stored Responses of the OpenAI Responses API as
// Dex Connector operations: the createResponse Mutation and the
// retrieveResponse Query.
//
// Text generation Steps use the llm connector,
// github.com/superdurable/dex-connectors-library/connectors/superdurable/llm,
// whose generateText Query also runs OpenAI models. Applications open a
// Connection once at startup with NewProjectConnection and wire
// openai.NewCreateResponseStep into a Flow, as the runnable example in
// examples/summarize-text does.
package openai

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// rateLimitHeaderNames are the OpenAI rate-limit headers every operation copies into Receipt metadata.
var rateLimitHeaderNames = []string{
	"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests",
	"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens",
}

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct{ httpClient *http.Client }

// WithHTTPClient overrides the default HTTP client, whose timeout is two
// minutes. createResponse and retrieveResponse use the client as given; the
// caller retains ownership.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated OpenAI requests for connector operations.
type Client struct {
	endpoint                    *url.URL
	model                       string
	httpClient                  *http.Client
	credentials                 sdkgo.CredentialProvider[Credentials]
	maxResponseBytes            int64
	maxSSEEventBytes            int
	checkpointHeartbeatInterval time.Duration
}

// StructuredOutput configures a JSON Schema response format for CreateRequest.
type StructuredOutput struct {
	// Name is the schema name sent to OpenAI.
	Name string `json:"name"`
	// Description optionally explains the schema to the model.
	Description string `json:"description,omitempty"`
	// Schema is the JSON Schema sent to OpenAI.
	Schema map[string]any `json:"schema"`
	// Strict requests exact schema adherence when true.
	Strict bool `json:"strict"`
}

// CreateRequest contains the provider request fields for create.
type CreateRequest struct {
	// Model is the OpenAI model ID for this response. Empty uses the connection's model.
	Model string
	// Input specifies input for create request.
	Input any
	// Instructions specifies instructions for create request.
	Instructions string
	// StructuredOutput specifies structured output for create request.
	StructuredOutput *StructuredOutput
}

// RetrieveRequest contains the provider request fields for retrieve.
type RetrieveRequest struct {
	// ResponseID is the OpenAI response identifier.
	ResponseID string
}

// Usage represents the connector's usage data.
type Usage struct {
	// InputTokens is the number of input tokens billed by the provider.
	InputTokens int `json:"inputTokens"`
	// CachedInputTokens is the cached subset of InputTokens.
	CachedInputTokens int `json:"cachedInputTokens"`
	// OutputTokens is the number of output tokens billed by the provider.
	OutputTokens int `json:"outputTokens"`
	// ReasoningOutputTokens is the reasoning subset of OutputTokens.
	ReasoningOutputTokens int `json:"reasoningOutputTokens"`
	// TotalTokens is the total number of billed tokens.
	TotalTokens int `json:"totalTokens"`
}

// Response contains the normalized fields returned by the OpenAI Responses API.
type Response struct {
	// ID is the stable provider identifier.
	ID string `json:"id"`
	// Model is the model returned by OpenAI.
	Model string `json:"model"`
	// Status is the status returned by OpenAI.
	Status string `json:"status"`
	// OutputText is the output text returned by OpenAI.
	OutputText string `json:"outputText"`
	// Usage is the usage returned by OpenAI.
	Usage Usage `json:"usage"`
}

// CreateResponseOperation implements the create connector operation.
type CreateResponseOperation struct{ client *Client }

// RetrieveResponseOperation implements the retrieve connector operation.
type RetrieveResponseOperation struct{ client *Client }

type requestFailure struct {
	failure sdkgo.Failure
}

// Error returns the safe human-readable failure message.
func (failure *requestFailure) Error() string { return failure.failure.Message }

// New validates configuration and constructs an authenticated OpenAI client.
// New trims surrounding whitespace from config.Model; a blank config.Model
// uses gpt-6-sol, which createResponse sends when its request names no model.
// Credentials are resolved again for every provider call. New returns an
// error for a nil credential provider, an invalid endpoint, model, or
// response limit, or a nil option; it makes no provider request.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	// Dex Web collects the model in a free-text field.
	config.Model = strings.TrimSpace(config.Model)
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !isPrintableModelID(config.Model) {
		return nil, fmt.Errorf("OpenAI model must be 1 to 256 printable ASCII characters without spaces")
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Hostname() == "" {
		return nil, fmt.Errorf("OpenAI endpoint must be absolute: %w", err)
	}
	if endpoint.Scheme != "https" && endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "localhost" {
		return nil, fmt.Errorf("OpenAI endpoint must use HTTPS")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("OpenAI connector option is nil")
		}
		option(&dependencies)
	}
	httpClient := dependencies.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	if config.MaxResponseBytes < 1 || config.MaxSSEEventBytes < 1 {
		return nil, fmt.Errorf("OpenAI response limits must be positive")
	}
	return &Client{
		endpoint: endpoint, model: config.Model, httpClient: httpClient, credentials: credentials,
		maxResponseBytes: config.MaxResponseBytes, maxSSEEventBytes: int(config.MaxSSEEventBytes),
		checkpointHeartbeatInterval: checkpointHeartbeatInterval,
	}, nil
}

// CreateResponse returns the CreateResponse operation bound to this client.
func (client *Client) CreateResponse() CreateResponseOperation {
	return CreateResponseOperation{client: client}
}

// RetrieveResponse returns the RetrieveResponse operation bound to this client.
func (client *Client) RetrieveResponse() RetrieveResponseOperation {
	return RetrieveResponseOperation{client: client}
}

// Definition returns the immutable connector operation definition.
func (CreateResponseOperation) Definition() sdkgo.MutationDefinition {
	return CreateResponseDefinition
}

// IdempotencyKey returns the stable connector Call ID. createResponse sends
// it as the X-Client-Request-Id header, which OpenAI logs for support but does
// not deduplicate on.
func (CreateResponseOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateRequest) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke executes one provider call and classifies its attempt. A request
// without a model sends the connection's model.
//
// Invoke fences retries with a Dex heartbeat checkpoint, so it must run in a
// Step with sync Execute durability that records no heartbeats of its own.
// Attempt 1 sends the request. A later attempt reads back a response whose ID
// an earlier attempt recorded, sends again only when the previous attempt
// proved that no response exists, and otherwise selects uncertain.
func (operation CreateResponseOperation) Invoke(call sdkgo.Call, input CreateRequest) sdkgo.MutationAttempt[Response] {
	model := cmp.Or(strings.TrimSpace(input.Model), operation.client.model)
	if failure := checkCreateInput(input.Input, model); failure != nil {
		return sdkgo.NewMutationBranch(CreateResponseBranchDefect, Response{}, failure, sdkgo.Receipt{})
	}
	payload := map[string]any{"model": model, "input": input.Input}
	if input.Instructions != "" {
		payload["instructions"] = input.Instructions
	}
	if input.StructuredOutput != nil {
		payload["text"] = map[string]any{"format": map[string]any{
			"type": "json_schema", "name": input.StructuredOutput.Name,
			"description": input.StructuredOutput.Description,
			"schema":      input.StructuredOutput.Schema, "strict": input.StructuredOutput.Strict,
		}}
	}
	streaming := call.HasProgressStream() || call.HasTextStream()
	if streaming {
		payload["stream"] = true
	}
	var previous createResponseCheckpoint
	hasPrevious, err := call.Context.GetLastHeartbeatValue(&previous)
	if err != nil || hasPrevious && previous.CallID != call.ID {
		failure := openAIFailure(sdkgo.FailureLocalDefect, "createResponse", "the Step's heartbeat value is not this call's checkpoint; createResponse must be the Step's only heartbeat writer")
		return sdkgo.NewMutationBranch(CreateResponseBranchDefect, Response{}, &failure, sdkgo.Receipt{})
	}
	heartbeat := newCreateResponseHeartbeat(call, operation.client.checkpointHeartbeatInterval)
	defer heartbeat.stop()
	attempt := call.Context.Attempt()
	switch {
	case hasPrevious && previous.ResponseID != "":
		if !isSafeResponseID(previous.ResponseID) {
			failure := openAIFailure(sdkgo.FailureLocalDefect, "createResponse", "saved response ID is invalid")
			return sdkgo.NewMutationBranch(CreateResponseBranchDefect, Response{}, &failure, sdkgo.Receipt{})
		}
		// The earlier checkpoint already holds the ID, so a failed record changes nothing.
		_ = heartbeat.begin(createResponseCheckpoint{CallID: call.ID, Attempt: attempt, ResponseID: previous.ResponseID})
		return operation.readBackResponse(call, heartbeat, previous.ResponseID, nil, "")
	case !hasPrevious && attempt == 1, hasPrevious && previous.CanResend && previous.Attempt == attempt-1:
		return operation.sendCreateRequest(call, heartbeat, payload, streaming)
	case !hasPrevious:
		return sdkgo.NewMutationUncertain(Response{}, openAIFailure(sdkgo.FailureTransport, "createResponse",
			"an earlier attempt left no checkpoint and may have created a response; createResponse requires sync Execute durability"),
			responseReceipt(call, "", nil, ""))
	default:
		return sdkgo.NewMutationUncertain(Response{}, openAIFailure(sdkgo.FailureTransport, "createResponse",
			"an earlier attempt may have created a response without recording its ID"), responseReceipt(call, "", nil, ""))
	}
}

// sendCreateRequest records the dispatch marker as the attempt's first
// heartbeat and then sends one create request.
func (operation CreateResponseOperation) sendCreateRequest(call sdkgo.Call, heartbeat *createResponseHeartbeat, payload map[string]any, streaming bool) sdkgo.MutationAttempt[Response] {
	request, failure := operation.client.newRequest(call, http.MethodPost, "/responses", string(call.ID), payload)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateResponseBranchDefect, Response{}, &failure.failure, sdkgo.Receipt{})
	}
	if err := heartbeat.begin(createResponseCheckpoint{CallID: call.ID, Attempt: call.Context.Attempt()}); err != nil {
		return retryCreateResponse(heartbeat, openAIFailure(sdkgo.FailureAvailability, "createResponse", "dispatch checkpoint could not be recorded"), 0)
	}
	var isDispatched atomic.Bool
	var hasTransportTrace atomic.Bool
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), &httptrace.ClientTrace{
		GetConn:      func(string) { hasTransportTrace.Store(true) },
		WroteHeaders: func() { isDispatched.Store(true) },
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			if info.Err == nil {
				isDispatched.Store(true)
			}
		},
	}))
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		if hasTransportTrace.Load() && !isDispatched.Load() {
			return retryCreateResponse(heartbeat, openAIFailure(sdkgo.FailureTransport, "createResponse", "request was not sent"), 0)
		}
		return sdkgo.NewMutationUncertain(Response{}, openAIFailure(sdkgo.FailureTransport, "createResponse", "provider outcome is unknown"), responseReceipt(call, "", http.Header{}, ""))
	}
	defer response.Body.Close()
	requestID := response.Header.Get("X-Request-Id")
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return createStatusAttempt(call, heartbeat, response.StatusCode, response.Header, requestID)
	}
	if streaming {
		return operation.readStream(call, heartbeat, response.Body, response.Header, requestID)
	}
	wire, readFailure := operation.client.readResponse(response.Body, "createResponse")
	if readFailure != nil {
		return sdkgo.NewMutationUncertain(Response{}, *readFailure, responseReceipt(call, requestID, response.Header, ""))
	}
	result := convertResponse(wire)
	if result.ID == "" {
		return sdkgo.NewMutationUncertain(Response{}, openAIFailure(sdkgo.FailureProtocol, "createResponse", "provider response has no ID"), responseReceipt(call, requestID, response.Header, ""))
	}
	if !isSafeResponseID(result.ID) {
		return sdkgo.NewMutationUncertain(Response{}, openAIFailure(sdkgo.FailureProtocol, "createResponse", "provider response ID is invalid"), responseReceipt(call, requestID, response.Header, ""))
	}
	// A lost checkpoint makes a later attempt select uncertain, never send again.
	_ = heartbeat.recordResponseID(result.ID)
	return operation.resolveKnownResponse(call, heartbeat, result, response.Header, requestID)
}

// retryCreateResponse lets the next attempt send again. Call it only when this
// attempt proved that no response exists.
func retryCreateResponse(heartbeat *createResponseHeartbeat, failure sdkgo.Failure, retryAfter time.Duration) sdkgo.MutationAttempt[Response] {
	// A lost checkpoint makes the next attempt select uncertain instead of sending.
	_ = heartbeat.recordCanResend()
	return sdkgo.NewMutationRetry[Response](failure, retryAfter)
}

func (operation CreateResponseOperation) resolveKnownResponse(call sdkgo.Call, heartbeat *createResponseHeartbeat, result Response, header http.Header, requestID string) sdkgo.MutationAttempt[Response] {
	receipt := responseReceipt(call, requestID, header, result.ID)
	switch result.Status {
	case "completed":
		return sdkgo.NewMutationBranch(CreateResponseBranchCompleted, result, nil, receipt)
	case "failed", "incomplete":
		failure := openAIFailure(sdkgo.FailureProviderRejection, "createResponse", "provider returned a "+result.Status+" response")
		return sdkgo.NewMutationBranch(CreateResponseBranchFailed, result, &failure, receipt)
	case "cancelled":
		// The connector never cancels; OpenAI cancels a synchronous response when its connection closes.
		return retryCreateResponse(heartbeat, openAIFailure(sdkgo.FailureAvailability, "createResponse", "response "+result.ID+" was cancelled before it completed"), 0)
	case "queued", "in_progress":
		return operation.readBackResponse(call, heartbeat, result.ID, header, requestID)
	default:
		failure := openAIFailure(sdkgo.FailureProtocol, "createResponse", "response "+result.ID+" has no recognized status")
		return sdkgo.NewMutationUncertain(result, failure, receipt)
	}
}

const (
	readBackInitialInterval = time.Second
	readBackMaxInterval     = 5 * time.Second
	readBackRequestTimeout  = 10 * time.Second
	// readBackDeadlineMargin leaves time to return Retry before Dex abandons the attempt.
	readBackDeadlineMargin        = 5 * time.Second
	readBackBudgetWithoutDeadline = 2 * time.Minute
)

// readBackResponse polls a queued or in-progress response until shortly before the attempt deadline, then returns Retry.
func (operation CreateResponseOperation) readBackResponse(call sdkgo.Call, heartbeat *createResponseHeartbeat, responseID string, createHeader http.Header, createRequestID string) sdkgo.MutationAttempt[Response] {
	pollDeadline := readBackDeadline(call.Context)
	interval := readBackInitialInterval
	for {
		if call.Context.Err() != nil {
			return sdkgo.NewMutationRetry[Response](openAIFailure(sdkgo.FailureAvailability, "createResponse", "response "+responseID+" read-back was interrupted"), 0)
		}
		read := operation.client.readStoredResponse(call, responseID, pollDeadline)
		switch {
		case read.retryFailure != nil:
			return sdkgo.NewMutationRetry[Response](*read.retryFailure, 0)
		case read.unconfirmedFailure != nil:
			return sdkgo.NewMutationUncertain(Response{ID: responseID}, *read.unconfirmedFailure, responseReceipt(call, createRequestID, createHeader, responseID))
		case read.isFound && read.response.Status != "queued" && read.response.Status != "in_progress":
			return operation.resolveKnownResponse(call, heartbeat, read.response, createHeader, createRequestID)
		}
		wait := max(interval, read.retryAfter)
		if time.Now().Add(wait).After(pollDeadline) {
			return sdkgo.NewMutationRetry[Response](openAIFailure(sdkgo.FailureAvailability, "createResponse", "response "+responseID+" is not confirmed yet"), read.retryAfter)
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-call.Context.Done():
			timer.Stop()
		}
		interval = min(2*interval, readBackMaxInterval)
	}
}

func readBackDeadline(ctx context.Context) time.Time {
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(readBackBudgetWithoutDeadline)
	}
	return deadline.Add(-readBackDeadlineMargin)
}

// storedResponseRead is one read-back GET. An empty value means keep polling.
type storedResponseRead struct {
	response   Response
	isFound    bool
	retryAfter time.Duration
	// retryFailure ends the attempt with Retry; unconfirmedFailure selects uncertain.
	retryFailure       *sdkgo.Failure
	unconfirmedFailure *sdkgo.Failure
}

func (client *Client) readStoredResponse(call sdkgo.Call, responseID string, pollDeadline time.Time) storedResponseRead {
	request, failure := client.newRequest(call, http.MethodGet, "/responses/"+url.PathEscape(responseID), "", nil)
	if failure != nil {
		retryFailure := openAIFailure(failure.failure.Kind, "createResponse", "response "+responseID+" read-back request could not be built")
		return storedResponseRead{retryFailure: &retryFailure}
	}
	requestDeadline := time.Now().Add(readBackRequestTimeout)
	if pollDeadline.Before(requestDeadline) {
		requestDeadline = pollDeadline
	}
	requestContext, cancel := context.WithDeadline(request.Context(), requestDeadline)
	defer cancel()
	response, err := client.httpClient.Do(request.WithContext(requestContext))
	if err != nil {
		return storedResponseRead{}
	}
	defer response.Body.Close()
	unconfirmed := func(kind sdkgo.FailureKind, message string) storedResponseRead {
		failure := openAIFailure(kind, "createResponse", "response "+responseID+" "+message)
		return storedResponseRead{unconfirmedFailure: &failure}
	}
	switch status := response.StatusCode; {
	case status >= 200 && status < 300:
		wire, readFailure := client.readResponse(response.Body, "createResponse")
		switch {
		case readFailure != nil && readFailure.Kind == sdkgo.FailureTransport:
			return storedResponseRead{}
		case readFailure != nil:
			return unconfirmed(readFailure.Kind, "read-back is unreadable: "+readFailure.Message)
		case wire.ID != responseID:
			return unconfirmed(sdkgo.FailureProtocol, "read-back returned another response ID")
		}
		return storedResponseRead{response: convertResponse(wire), isFound: true}
	case status == http.StatusNotFound:
		return unconfirmed(sdkgo.FailureNotFound, "was not found on read-back, so its outcome cannot be confirmed")
	case status == http.StatusUnauthorized:
		return unconfirmed(sdkgo.FailureAuthentication, "read-back returned HTTP 401; the API key must be valid and allowed to read responses with GET /v1/responses/{id}")
	case status == http.StatusForbidden:
		return unconfirmed(sdkgo.FailureAuthorization, "read-back returned HTTP 403; the API key needs read permission for responses (GET /v1/responses/{id})")
	case status == http.StatusTooManyRequests:
		return storedResponseRead{retryAfter: retryAfterHeader(response.Header)}
	case status >= 500, status == http.StatusRequestTimeout, status == http.StatusConflict:
		// A repeated GET is safe; the attempt deadline bounds polling.
		return storedResponseRead{}
	default:
		return unconfirmed(sdkgo.FailureProviderRejection, "read-back returned HTTP "+strconv.Itoa(status))
	}
}

func isSafeResponseID(responseID string) bool {
	if len(responseID) < 1 || len(responseID) > 128 {
		return false
	}
	for index := 0; index < len(responseID); index++ {
		character := responseID[index]
		isDigit := character >= '0' && character <= '9'
		isLetter := character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
		if !isDigit && !isLetter && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

// Definition returns the immutable connector operation definition.
func (RetrieveResponseOperation) Definition() sdkgo.QueryDefinition {
	return RetrieveResponseDefinition
}

// Invoke executes one provider call and classifies its attempt.
func (operation RetrieveResponseOperation) Invoke(call sdkgo.Call, input RetrieveRequest) sdkgo.QueryAttempt[Response] {
	if input.ResponseID == "" {
		failure := openAIFailure(sdkgo.FailureValidation, "retrieveResponse", "response ID is required")
		return sdkgo.NewQueryBranch(RetrieveResponseBranchDefect, Response{}, &failure, sdkgo.Receipt{})
	}
	request, failure := operation.client.newRequest(call, http.MethodGet, "/responses/"+url.PathEscape(input.ResponseID), "", nil)
	if failure != nil {
		return sdkgo.NewQueryBranch(RetrieveResponseBranchDefect, Response{}, &failure.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[Response](openAIFailure(sdkgo.FailureAvailability, "retrieveResponse", "provider is unavailable"), 0)
	}
	defer response.Body.Close()
	requestID := response.Header.Get("X-Request-Id")
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failure, retryAfter, isRetryable := classifyOpenAIStatus("retrieveResponse", response.StatusCode, response.Header)
		if isRetryable {
			return sdkgo.NewQueryRetry[Response](failure, retryAfter)
		}
		branch := RetrieveResponseBranchProviderRejected
		if response.StatusCode == http.StatusNotFound {
			branch = RetrieveResponseBranchNotFound
		}
		return sdkgo.NewQueryBranch(branch, Response{}, &failure, responseReceipt(call, requestID, response.Header, input.ResponseID))
	}
	wire, readFailure := operation.client.readResponse(response.Body, "retrieveResponse")
	if readFailure != nil {
		if readFailure.Kind == sdkgo.FailureResponseTooLarge || readFailure.Kind == sdkgo.FailureProtocol {
			return sdkgo.NewQueryBranch(RetrieveResponseBranchInvalidResponse, Response{}, readFailure, responseReceipt(call, requestID, response.Header, input.ResponseID))
		}
		return sdkgo.NewQueryRetry[Response](*readFailure, 0)
	}
	result := convertResponse(wire)
	return sdkgo.NewQueryBranch(RetrieveResponseBranchFound, result, nil, responseReceipt(call, requestID, response.Header, result.ID))
}

func (client *Client) newRequest(call sdkgo.Call, method, path string, clientRequestID string, payload any) (*http.Request, *requestFailure) {
	credential, err := client.credentials.Resolve(call)
	if err != nil {
		return nil, &requestFailure{failure: openAIFailure(sdkgo.FailureAuthentication, call.Operation.OperationID, "connection credentials are unavailable")}
	}
	if err := credential.Validate(); err != nil {
		return nil, &requestFailure{failure: openAIFailure(sdkgo.FailureAuthentication, call.Operation.OperationID, "connection credentials are invalid")}
	}
	apiKey := credential.APIKey.Reveal()
	var body io.Reader
	if payload != nil {
		encoded, encodeErr := json.Marshal(payload)
		if encodeErr != nil {
			return nil, &requestFailure{failure: openAIFailure(sdkgo.FailureValidation, call.Operation.OperationID, notSerializableMessage)}
		}
		body = bytes.NewReader(encoded)
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + path
	request, err := http.NewRequestWithContext(call.Context, method, target, body)
	if err != nil {
		return nil, &requestFailure{failure: openAIFailure(sdkgo.FailureLocalDefect, call.Operation.OperationID, "request could not be built")}
	}
	if body != nil {
		// Without GetBody, net/http never replays a create that may have reached OpenAI.
		request.GetBody = nil
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	if clientRequestID != "" {
		request.Header.Set("X-Client-Request-Id", clientRequestID)
	}
	return request, nil
}

func (client *Client) readResponse(body io.Reader, operation string) (wireResponse, *sdkgo.Failure) {
	data, err := io.ReadAll(io.LimitReader(body, client.maxResponseBytes+1))
	if err != nil {
		failure := openAIFailure(sdkgo.FailureTransport, operation, "provider response could not be read")
		return wireResponse{}, &failure
	}
	if int64(len(data)) > client.maxResponseBytes {
		failure := openAIFailure(sdkgo.FailureResponseTooLarge, operation, "provider response exceeds the configured size limit")
		return wireResponse{}, &failure
	}
	var wire wireResponse
	if err := json.Unmarshal(data, &wire); err != nil {
		failure := openAIFailure(sdkgo.FailureProtocol, operation, "provider response is invalid")
		return wireResponse{}, &failure
	}
	return wire, nil
}

func createStatusAttempt(call sdkgo.Call, heartbeat *createResponseHeartbeat, status int, header http.Header, requestID string) sdkgo.MutationAttempt[Response] {
	failure, retryAfter, isRetryable := classifyOpenAIStatus("createResponse", status, header)
	if isRetryable {
		return retryCreateResponse(heartbeat, failure, retryAfter)
	}
	receipt := responseReceipt(call, requestID, header, "")
	switch {
	case status >= 500:
		return sdkgo.NewMutationUncertain(Response{}, failure, receipt)
	case status == http.StatusRequestTimeout:
		// A timeout reply does not prove that no response was stored.
		return sdkgo.NewMutationUncertain(Response{}, openAIFailure(sdkgo.FailureTransport, "createResponse",
			"provider returned HTTP 408 without a response ID, so a response may exist"), receipt)
	case status == http.StatusConflict:
		// OpenAI offers no key or read-back that could resolve a conflicting create.
		return sdkgo.NewMutationUncertain(Response{}, openAIFailure(sdkgo.FailureConflict, "createResponse",
			"provider returned HTTP 409 without a response ID, so a response may exist"), receipt)
	}
	return sdkgo.NewMutationBranch(CreateResponseBranchProviderRejected, Response{}, &failure, receipt)
}

func classifyOpenAIStatus(operation string, status int, header http.Header) (sdkgo.Failure, time.Duration, bool) {
	kind := sdkgo.FailureProviderRejection
	isRetryable := false
	switch status {
	case http.StatusUnauthorized:
		kind = sdkgo.FailureAuthentication
	case http.StatusForbidden:
		kind = sdkgo.FailureAuthorization
	case http.StatusNotFound:
		kind = sdkgo.FailureNotFound
	case http.StatusRequestTimeout:
		kind = sdkgo.FailureAvailability
		isRetryable = operation == "retrieveResponse"
	case http.StatusConflict:
		kind = sdkgo.FailureConflict
	case http.StatusTooManyRequests:
		kind = sdkgo.FailureRateLimit
		isRetryable = true
	case http.StatusServiceUnavailable:
		kind = sdkgo.FailureAvailability
		isRetryable = true
	default:
		if status >= 500 {
			kind = sdkgo.FailureAvailability
			isRetryable = operation == "retrieveResponse"
		}
	}
	var retryAfter time.Duration
	if kind == sdkgo.FailureRateLimit {
		retryAfter = retryAfterHeader(header)
	}
	return openAIFailure(kind, operation, "provider returned HTTP "+strconv.Itoa(status)), retryAfter, isRetryable
}

// retryAfterHeader returns Retry-After in delay-seconds or HTTP-date form, capped at one hour, or zero.
func retryAfterHeader(header http.Header) time.Duration {
	return providerhttp.ParseRetryAfter(header.Get("Retry-After"), time.Now())
}

func openAIFailure(kind sdkgo.FailureKind, operation, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "openai", Operation: operation, Message: message}
}

func responseReceipt(call sdkgo.Call, requestID string, header http.Header, responseID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "openai",
		ProviderObjectID: responseID, ProviderRequestID: requestID,
		ObservedAt: time.Now().UTC(), Metadata: rateLimitMetadata(header),
	}
}

type wireResponse struct {
	ID                string          `json:"id"`
	Model             string          `json:"model"`
	Status            string          `json:"status"`
	Error             json.RawMessage `json:"error"`
	IncompleteDetails *struct {
		Reason string `json:"reason"`
	} `json:"incomplete_details"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Refusal string `json:"refusal"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
		InputDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		OutputDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"output_tokens_details"`
	} `json:"usage"`
}

func convertResponse(wire wireResponse) Response {
	var texts []string
	for _, output := range wire.Output {
		for _, content := range output.Content {
			if content.Type == "output_text" {
				texts = append(texts, content.Text)
			}
		}
	}
	return Response{
		ID: wire.ID, Model: wire.Model, Status: wire.Status, OutputText: strings.Join(texts, ""),
		Usage: Usage{
			InputTokens: wire.Usage.InputTokens, CachedInputTokens: wire.Usage.InputDetails.CachedTokens,
			OutputTokens: wire.Usage.OutputTokens, ReasoningOutputTokens: wire.Usage.OutputDetails.ReasoningTokens,
			TotalTokens: wire.Usage.TotalTokens,
		},
	}
}

func rateLimitMetadata(header http.Header) map[string]string {
	metadata := map[string]string{}
	for _, name := range rateLimitHeaderNames {
		if value := header.Get(name); value != "" {
			metadata[name] = value
		}
	}
	return metadata
}

// isPrintableModelID accepts 1 to 256 printable ASCII characters without spaces, which every OpenAI model ID is.
func isPrintableModelID(model string) bool {
	if len(model) < 1 || len(model) > 256 {
		return false
	}
	for index := 0; index < len(model); index++ {
		if model[index] < 0x21 || model[index] > 0x7E {
			return false
		}
	}
	return true
}
