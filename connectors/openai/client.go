// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package openai implements the OpenAI Responses API sdkgo.
package openai

import (
	"bytes"
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

type Option func(*clientOptions)

type clientOptions struct{ httpClient *http.Client }

func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

type Client struct {
	endpoint         *url.URL
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
	maxSSEEventBytes int
}

type StructuredOutput struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema"`
	Strict      bool           `json:"strict"`
}

type CreateRequest struct {
	Model            string
	Input            any
	Instructions     string
	StructuredOutput *StructuredOutput
}

type RetrieveRequest struct {
	ResponseID string
}

type Usage struct {
	InputTokens           int `json:"inputTokens"`
	CachedInputTokens     int `json:"cachedInputTokens"`
	OutputTokens          int `json:"outputTokens"`
	ReasoningOutputTokens int `json:"reasoningOutputTokens"`
	TotalTokens           int `json:"totalTokens"`
}

type Response struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	Status     string `json:"status"`
	OutputText string `json:"outputText"`
	Usage      Usage  `json:"usage"`
}

type CreateResponseOperation struct{ client *Client }

type RetrieveResponseOperation struct{ client *Client }

type requestFailure struct {
	failure sdkgo.Failure
}

func (failure *requestFailure) Error() string { return failure.failure.Message }

func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
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
		endpoint: endpoint, httpClient: httpClient, credentials: credentials,
		maxResponseBytes: config.MaxResponseBytes, maxSSEEventBytes: int(config.MaxSSEEventBytes),
	}, nil
}

func (client *Client) CreateResponse() CreateResponseOperation {
	return CreateResponseOperation{client: client}
}

func (client *Client) RetrieveResponse() RetrieveResponseOperation {
	return RetrieveResponseOperation{client: client}
}

func (CreateResponseOperation) Definition() sdkgo.MutationDefinition {
	return CreateResponseDefinition
}

func (CreateResponseOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateRequest) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

func (operation CreateResponseOperation) Invoke(call sdkgo.Call, input CreateRequest) sdkgo.MutationAttempt[Response] {
	if input.Model == "" || input.Input == nil {
		failure := openAIFailure(sdkgo.FailureValidation, "createResponse", "model and input are required")
		return sdkgo.NewMutationBranch(CreateResponseBranchDefect, Response{}, &failure, sdkgo.Receipt{})
	}
	payload := map[string]any{"model": input.Model, "input": input.Input}
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
		branch := CreateResponseBranchFailed
		if failure.failure.Kind == sdkgo.FailureLocalDefect || failure.failure.Kind == sdkgo.FailureValidation {
			branch = CreateResponseBranchDefect
		}
		return sdkgo.NewMutationBranch(branch, Response{}, &failure.failure, sdkgo.Receipt{})
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

func (RetrieveResponseOperation) Definition() sdkgo.QueryDefinition {
	return RetrieveResponseDefinition
}

func (operation RetrieveResponseOperation) Invoke(call sdkgo.Call, input RetrieveRequest) sdkgo.QueryAttempt[Response] {
	if input.ResponseID == "" {
		failure := openAIFailure(sdkgo.FailureValidation, "retrieveResponse", "response ID is required")
		return sdkgo.NewQueryBranch(RetrieveResponseBranchDefect, Response{}, &failure, sdkgo.Receipt{})
	}
	request, failure := operation.client.newRequest(call, http.MethodGet, "/responses/"+url.PathEscape(input.ResponseID), "", nil)
	if failure != nil {
		branch := RetrieveResponseBranchFailed
		if failure.failure.Kind == sdkgo.FailureLocalDefect || failure.failure.Kind == sdkgo.FailureValidation {
			branch = RetrieveResponseBranchDefect
		}
		return sdkgo.NewQueryBranch(branch, Response{}, &failure.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[Response](openAIFailure(sdkgo.FailureAvailability, "retrieveResponse", "provider is unavailable"), 0)
	}
	defer response.Body.Close()
	requestID := response.Header.Get("X-Request-Id")
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failure, retryAfter, retry := classifyOpenAIStatus("retrieveResponse", response.StatusCode, response.Header)
		if retry {
			return sdkgo.NewQueryRetry[Response](failure, retryAfter)
		}
		return sdkgo.NewQueryBranch(RetrieveResponseBranchFailed, Response{}, &failure, responseReceipt(call, requestID, response.Header, input.ResponseID))
	}
	wire, readFailure := operation.client.readResponse(response.Body, "retrieveResponse")
	if readFailure != nil {
		if readFailure.Kind == sdkgo.FailureResponseTooLarge || readFailure.Kind == sdkgo.FailureProtocol {
			return sdkgo.NewQueryBranch(RetrieveResponseBranchFailed, Response{}, readFailure, responseReceipt(call, requestID, response.Header, input.ResponseID))
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
	failure, retryAfter, retry := classifyOpenAIStatus("createResponse", status, header)
	if retry {
		return sdkgo.NewMutationRetry[Response](failure, retryAfter)
	}
	receipt := responseReceipt(call, requestID, header, "")
	if status >= 500 {
		return sdkgo.NewMutationUncertain(Response{}, failure, receipt)
	}
	return sdkgo.NewMutationBranch(CreateResponseBranchFailed, Response{}, &failure, receipt)
}

func classifyOpenAIStatus(operation string, status int, header http.Header) (sdkgo.Failure, time.Duration, bool) {
	kind := sdkgo.FailureProviderRejection
	retry := false
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
		retry = true
	default:
		if status >= 500 {
			kind = sdkgo.FailureAvailability
			retry = operation == "retrieveResponse"
		}
	}
	var retryAfter time.Duration
	if kind == sdkgo.FailureRateLimit {
		if seconds, err := strconv.Atoi(header.Get("Retry-After")); err == nil && seconds > 0 {
			retryAfter = time.Duration(seconds) * time.Second
		}
	}
	return openAIFailure(kind, operation, "provider returned HTTP "+strconv.Itoa(status)), retryAfter, retry
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
	ID     string `json:"id"`
	Model  string `json:"model"`
	Status string `json:"status"`
	Output []struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
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
	for _, name := range []string{
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens",
	} {
		if value := header.Get(name); value != "" {
			metadata[name] = value
		}
	}
	return metadata
}
