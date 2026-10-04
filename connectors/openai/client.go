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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
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
	endpoint         *url.URL
	model            string
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
	maxSSEEventBytes int
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

// IdempotencyKey derives the provider key from the stable connector call ID.
func (CreateResponseOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateRequest) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke executes one provider call and classifies its attempt. A request
// without a model sends the connection's model.
func (operation CreateResponseOperation) Invoke(call sdkgo.Call, input CreateRequest) sdkgo.MutationAttempt[Response] {
	model := cmp.Or(strings.TrimSpace(input.Model), operation.client.model)
	if input.Input == nil || !isPrintableModelID(model) {
		failure := openAIFailure(sdkgo.FailureValidation, "createResponse", "input and a model of printable ASCII without spaces are required")
		return sdkgo.NewMutationBranch(CreateResponseBranchDefect, Response{}, &failure, sdkgo.Receipt{})
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
	request, failure := operation.client.newRequest(call, http.MethodPost, "/responses", call.IdempotencyKey, payload)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateResponseBranchDefect, Response{}, &failure.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationUncertain(Response{}, openAIFailure(sdkgo.FailureTransport, "createResponse", "provider outcome is unknown"), responseReceipt(call, "", http.Header{}, ""))
	}
	defer response.Body.Close()
	requestID := response.Header.Get("X-Request-Id")
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return createStatusAttempt(call, response.StatusCode, response.Header, requestID)
	}
	if streaming {
		return operation.readStream(call, response.Body, response.Header, requestID)
	}
	wire, readFailure := operation.client.readResponse(response.Body, "createResponse")
	if readFailure != nil {
		return sdkgo.NewMutationUncertain(Response{}, *readFailure, responseReceipt(call, requestID, response.Header, ""))
	}
	result := convertResponse(wire)
	return sdkgo.NewMutationBranch(CreateResponseBranchCompleted, result, nil, responseReceipt(call, requestID, response.Header, result.ID))
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

func (client *Client) newRequest(call sdkgo.Call, method, path string, key sdkgo.IdempotencyKey, payload any) (*http.Request, *requestFailure) {
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
			return nil, &requestFailure{failure: openAIFailure(sdkgo.FailureValidation, call.Operation.OperationID, "request is not JSON serializable")}
		}
		body = bytes.NewReader(encoded)
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + path
	request, err := http.NewRequestWithContext(call.Context, method, target, body)
	if err != nil {
		return nil, &requestFailure{failure: openAIFailure(sdkgo.FailureLocalDefect, call.Operation.OperationID, "request could not be built")}
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Idempotency-Key", string(key))
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

func createStatusAttempt(call sdkgo.Call, status int, header http.Header, requestID string) sdkgo.MutationAttempt[Response] {
	failure, retryAfter, isRetryable := classifyOpenAIStatus("createResponse", status, header)
	if isRetryable {
		return sdkgo.NewMutationRetry[Response](failure, retryAfter)
	}
	receipt := responseReceipt(call, requestID, header, "")
	if status >= 500 {
		return sdkgo.NewMutationUncertain(Response{}, failure, receipt)
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
	case http.StatusConflict:
		kind = sdkgo.FailureConflict
	case http.StatusTooManyRequests:
		kind = sdkgo.FailureRateLimit
		isRetryable = true
	default:
		if status >= 500 {
			kind = sdkgo.FailureAvailability
			isRetryable = operation == "retrieveResponse"
		}
	}
	var retryAfter time.Duration
	if kind == sdkgo.FailureRateLimit {
		if seconds, err := strconv.Atoi(header.Get("Retry-After")); err == nil && seconds > 0 {
			retryAfter = time.Duration(seconds) * time.Second
		}
	}
	return openAIFailure(kind, operation, "provider returned HTTP "+strconv.Itoa(status)), retryAfter, isRetryable
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
