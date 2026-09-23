// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package openai implements the OpenAI Responses API connector.
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

	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

type Option func(*clientOptions)

type clientOptions struct{ httpClient *http.Client }

func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

type Client struct {
	endpoint         *url.URL
	httpClient       *http.Client
	credentials      connector.CredentialProvider[Credentials]
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
	failure connector.Failure
}

func (failure *requestFailure) Error() string { return failure.failure.Message }

func New(config Config, credentials connector.CredentialProvider[Credentials], options ...Option) (*Client, error) {
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

func (CreateResponseOperation) Definition() connector.MutationDefinition {
	return CreateResponseDefinition
}

func (CreateResponseOperation) IdempotencyKey(callID connector.CallID, _ CreateRequest) connector.IdempotencyKey {
	return connector.IdempotencyKey(callID)
}

func (operation CreateResponseOperation) Invoke(call connector.Call, input CreateRequest) connector.MutationAttempt[Response] {
	if input.Model == "" || input.Input == nil {
		failure := openAIFailure(connector.FailureValidation, "createResponse", "model and input are required")
		return connector.NewMutationBranch(CreateResponseBranchDefect, Response{}, &failure, connector.Receipt{})
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
		if failure.failure.Kind == connector.FailureLocalDefect || failure.failure.Kind == connector.FailureValidation {
			branch = CreateResponseBranchDefect
		}
		return connector.NewMutationBranch(branch, Response{}, &failure.failure, connector.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return connector.NewMutationUncertain(Response{}, openAIFailure(connector.FailureTransport, "createResponse", "provider outcome is unknown"), responseReceipt(call, "", http.Header{}, ""))
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
		return connector.NewMutationUncertain(Response{}, *readFailure, responseReceipt(call, requestID, response.Header, ""))
	}
	result := convertResponse(wire)
	return connector.NewMutationBranch(CreateResponseBranchCompleted, result, nil, responseReceipt(call, requestID, response.Header, result.ID))
}

func (RetrieveResponseOperation) Definition() connector.QueryDefinition {
	return RetrieveResponseDefinition
}

func (operation RetrieveResponseOperation) Invoke(call connector.Call, input RetrieveRequest) connector.QueryAttempt[Response] {
	if input.ResponseID == "" {
		failure := openAIFailure(connector.FailureValidation, "retrieveResponse", "response ID is required")
		return connector.NewQueryBranch(RetrieveResponseBranchDefect, Response{}, &failure, connector.Receipt{})
	}
	request, failure := operation.client.newRequest(call, http.MethodGet, "/responses/"+url.PathEscape(input.ResponseID), "", nil)
	if failure != nil {
		branch := RetrieveResponseBranchFailed
		if failure.failure.Kind == connector.FailureLocalDefect || failure.failure.Kind == connector.FailureValidation {
			branch = RetrieveResponseBranchDefect
		}
		return connector.NewQueryBranch(branch, Response{}, &failure.failure, connector.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return connector.NewQueryRetry[Response](openAIFailure(connector.FailureAvailability, "retrieveResponse", "provider is unavailable"), 0)
	}
	defer response.Body.Close()
	requestID := response.Header.Get("X-Request-Id")
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failure, retryAfter, retry := classifyOpenAIStatus("retrieveResponse", response.StatusCode, response.Header)
		if retry {
			return connector.NewQueryRetry[Response](failure, retryAfter)
		}
		return connector.NewQueryBranch(RetrieveResponseBranchFailed, Response{}, &failure, responseReceipt(call, requestID, response.Header, input.ResponseID))
	}
	wire, readFailure := operation.client.readResponse(response.Body, "retrieveResponse")
	if readFailure != nil {
		if readFailure.Kind == connector.FailureResponseTooLarge || readFailure.Kind == connector.FailureProtocol {
			return connector.NewQueryBranch(RetrieveResponseBranchFailed, Response{}, readFailure, responseReceipt(call, requestID, response.Header, input.ResponseID))
		}
		return connector.NewQueryRetry[Response](*readFailure, 0)
	}
	result := convertResponse(wire)
	return connector.NewQueryBranch(RetrieveResponseBranchFound, result, nil, responseReceipt(call, requestID, response.Header, result.ID))
}

func (client *Client) newRequest(call connector.Call, method, path string, key connector.IdempotencyKey, payload any) (*http.Request, *requestFailure) {
	credential, err := client.credentials.Resolve(call)
	if err != nil {
		return nil, &requestFailure{failure: openAIFailure(connector.FailureAuthentication, call.Operation.OperationID, "connection credentials are unavailable")}
	}
	if err := credential.Validate(); err != nil {
		return nil, &requestFailure{failure: openAIFailure(connector.FailureAuthentication, call.Operation.OperationID, "connection credentials are invalid")}
	}
	apiKey := credential.APIKey.Reveal()
	var body io.Reader
	if payload != nil {
		encoded, encodeErr := json.Marshal(payload)
		if encodeErr != nil {
			return nil, &requestFailure{failure: openAIFailure(connector.FailureValidation, call.Operation.OperationID, "request is not JSON serializable")}
		}
		body = bytes.NewReader(encoded)
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + path
	request, err := http.NewRequestWithContext(call.Context, method, target, body)
	if err != nil {
		return nil, &requestFailure{failure: openAIFailure(connector.FailureLocalDefect, call.Operation.OperationID, "request could not be built")}
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Idempotency-Key", string(key))
	}
	return request, nil
}

func (client *Client) readResponse(body io.Reader, operation string) (wireResponse, *connector.Failure) {
	data, err := io.ReadAll(io.LimitReader(body, client.maxResponseBytes+1))
	if err != nil {
		failure := openAIFailure(connector.FailureTransport, operation, "provider response could not be read")
		return wireResponse{}, &failure
	}
	if int64(len(data)) > client.maxResponseBytes {
		failure := openAIFailure(connector.FailureResponseTooLarge, operation, "provider response exceeds the configured size limit")
		return wireResponse{}, &failure
	}
	var wire wireResponse
	if err := json.Unmarshal(data, &wire); err != nil {
		failure := openAIFailure(connector.FailureProtocol, operation, "provider response is invalid")
		return wireResponse{}, &failure
	}
	return wire, nil
}

func createStatusAttempt(call connector.Call, status int, header http.Header, requestID string) connector.MutationAttempt[Response] {
	failure, retryAfter, retry := classifyOpenAIStatus("createResponse", status, header)
	if retry {
		return connector.NewMutationRetry[Response](failure, retryAfter)
	}
	receipt := responseReceipt(call, requestID, header, "")
	if status >= 500 {
		return connector.NewMutationUncertain(Response{}, failure, receipt)
	}
	return connector.NewMutationBranch(CreateResponseBranchFailed, Response{}, &failure, receipt)
}

func classifyOpenAIStatus(operation string, status int, header http.Header) (connector.Failure, time.Duration, bool) {
	kind := connector.FailureProviderRejection
	retry := false
	switch status {
	case http.StatusUnauthorized:
		kind = connector.FailureAuthentication
	case http.StatusForbidden:
		kind = connector.FailureAuthorization
	case http.StatusNotFound:
		kind = connector.FailureNotFound
	case http.StatusConflict:
		kind = connector.FailureConflict
	case http.StatusTooManyRequests:
		kind = connector.FailureRateLimit
		retry = true
	default:
		if status >= 500 {
			kind = connector.FailureAvailability
			retry = operation == "retrieveResponse"
		}
	}
	var retryAfter time.Duration
	if kind == connector.FailureRateLimit {
		if seconds, err := strconv.Atoi(header.Get("Retry-After")); err == nil && seconds > 0 {
			retryAfter = time.Duration(seconds) * time.Second
		}
	}
	return openAIFailure(kind, operation, "provider returned HTTP "+strconv.Itoa(status)), retryAfter, retry
}

func openAIFailure(kind connector.FailureKind, operation, message string) connector.Failure {
	return connector.Failure{Kind: kind, Provider: "openai", Operation: operation, Message: message}
}

func responseReceipt(call connector.Call, requestID string, header http.Header, responseID string) connector.Receipt {
	return connector.Receipt{
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
