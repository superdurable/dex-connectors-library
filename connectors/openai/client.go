// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package openai implements the OpenAI Responses API connector.
package openai

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

const defaultEndpoint = "https://api.openai.com/v1"

var (
	createResponseDefinition   = connector.MutationDefinition{Operation: connector.OperationRef{ConnectorID: "openai", OperationID: "createResponse"}}
	retrieveResponseDefinition = connector.QueryDefinition{Operation: connector.OperationRef{ConnectorID: "openai", OperationID: "retrieveResponse"}}
)

type Config struct {
	Endpoint string
	Client   *http.Client
}

type Client struct {
	endpoint    *url.URL
	httpClient  *http.Client
	credentials connector.CredentialProvider
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

func New(config Config, credentials connector.CredentialProvider) (*Client, error) {
	endpointText := config.Endpoint
	if endpointText == "" {
		endpointText = defaultEndpoint
	}
	endpoint, err := url.Parse(endpointText)
	if err != nil || endpoint.Scheme == "" || endpoint.Hostname() == "" {
		return nil, fmt.Errorf("OpenAI endpoint must be absolute: %w", err)
	}
	if endpoint.Scheme != "https" && endpoint.Hostname() != "127.0.0.1" && endpoint.Hostname() != "localhost" {
		return nil, fmt.Errorf("OpenAI endpoint must use HTTPS")
	}
	httpClient := config.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 2 * time.Minute}
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	return &Client{endpoint: endpoint, httpClient: httpClient, credentials: credentials}, nil
}

func (client *Client) CreateResponse() CreateResponseOperation {
	return CreateResponseOperation{client: client}
}

func (client *Client) RetrieveResponse() RetrieveResponseOperation {
	return RetrieveResponseOperation{client: client}
}

func (CreateResponseOperation) Definition() connector.MutationDefinition {
	return createResponseDefinition
}

func (operation CreateResponseOperation) Invoke(call connector.Call, input CreateRequest) (connector.MutationResult[Response], error) {
	if input.Model == "" || input.Input == nil {
		return connector.MutationResult[Response]{}, validationError("createResponse", "model and input are required", nil)
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
	var wire wireResponse
	requestID, headers, dispatched, err := operation.client.do(call, http.MethodPost, "/responses", call.ID, payload, &wire)
	if err != nil {
		if !dispatched {
			return connector.MutationResult[Response]{}, err
		}
		var typed *connector.Error
		if errors.As(err, &typed) {
			return connector.MutationResult[Response]{}, typed
		}
		receipt := connector.Receipt{CallID: call.ID, Provider: "openai", ObservedAt: time.Now().UTC()}
		return connector.MutationResult[Response]{}, connector.NewError(connector.ErrorUnknownMutation, "openai", "createResponse", "provider outcome is unknown", err).WithReceipt(receipt)
	}
	result := convertResponse(wire)
	return connector.MutationResult[Response]{
		Outcome: connector.MutationSucceeded,
		Value:   result,
		Receipt: connector.Receipt{
			CallID: call.ID, Provider: "openai", ProviderObjectID: result.ID,
			ProviderRequestID: requestID, ObservedAt: time.Now().UTC(), Metadata: rateLimitMetadata(headers),
		},
	}, nil
}

func (RetrieveResponseOperation) Definition() connector.QueryDefinition {
	return retrieveResponseDefinition
}

func (operation RetrieveResponseOperation) Invoke(call connector.Call, input RetrieveRequest) (connector.QueryResult[Response], error) {
	if input.ResponseID == "" {
		return connector.QueryResult[Response]{}, validationError("retrieveResponse", "response ID is required", nil)
	}
	var wire wireResponse
	requestID, headers, dispatched, err := operation.client.do(call, http.MethodGet, "/responses/"+url.PathEscape(input.ResponseID), "", nil, &wire)
	if err != nil {
		if !dispatched {
			return connector.QueryResult[Response]{}, err
		}
		var typed *connector.Error
		if errors.As(err, &typed) {
			return connector.QueryResult[Response]{}, typed
		}
		return connector.QueryResult[Response]{}, connector.NewError(connector.ErrorRetryableAvailability, "openai", "retrieveResponse", "provider is unavailable", err)
	}
	return connector.QueryResult[Response]{
		Value: convertResponse(wire),
		Receipt: connector.Receipt{
			CallID: call.ID, Provider: "openai", ProviderObjectID: input.ResponseID,
			ProviderRequestID: requestID, ObservedAt: time.Now().UTC(), Metadata: rateLimitMetadata(headers),
		},
	}, nil
}

func (client *Client) do(call connector.Call, method, path string, callID connector.CallID, payload any, output any) (string, http.Header, bool, error) {
	credential, err := client.credentials.Resolve(call)
	if err != nil {
		return "", nil, false, err
	}
	apiKey, err := connector.RequiredCredentialValue(credential, "api_key")
	if err != nil {
		return "", nil, false, connector.NewError(connector.ErrorAuthentication, "openai", path, "API key is not configured", err)
	}
	var body io.Reader
	if payload != nil {
		encoded, encodeErr := json.Marshal(payload)
		if encodeErr != nil {
			return "", nil, false, validationError(path, "request is not JSON serializable", encodeErr)
		}
		body = bytes.NewReader(encoded)
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + path
	request, err := http.NewRequestWithContext(call.Context, method, target, body)
	if err != nil {
		return "", nil, false, validationError(path, "build request", err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	if callID != "" {
		request.Header.Set("Idempotency-Key", string(callID))
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", nil, true, err
	}
	defer response.Body.Close()
	requestID := response.Header.Get("X-Request-Id")
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return requestID, response.Header, true, openAIStatusError(path, response.StatusCode, response.Header, requestID, call, callID != "")
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if err := decoder.Decode(output); err != nil {
		return requestID, response.Header, true, fmt.Errorf("decode provider response: %w", err)
	}
	return requestID, response.Header, true, nil
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

func validationError(operation, message string, cause error) error {
	return connector.NewError(connector.ErrorValidation, "openai", operation, message, cause)
}

func openAIStatusError(operation string, status int, header http.Header, requestID string, call connector.Call, mutation bool) error {
	kind := connector.ErrorTerminalRejection
	switch status {
	case http.StatusUnauthorized:
		kind = connector.ErrorAuthentication
	case http.StatusForbidden:
		kind = connector.ErrorAuthorization
	case http.StatusNotFound:
		kind = connector.ErrorNotFound
	case http.StatusConflict:
		kind = connector.ErrorConflict
	case http.StatusTooManyRequests:
		kind = connector.ErrorRateLimit
	default:
		if status >= 500 {
			if mutation {
				kind = connector.ErrorUnknownMutation
			} else {
				kind = connector.ErrorRetryableAvailability
			}
		}
	}
	err := connector.NewError(kind, "openai", operation, "provider returned HTTP "+strconv.Itoa(status), nil)
	if kind == connector.ErrorRateLimit {
		if seconds, parseErr := strconv.Atoi(header.Get("Retry-After")); parseErr == nil {
			err.WithRetryAfter(time.Duration(seconds) * time.Second)
		}
	}
	if mutation && kind != connector.ErrorRateLimit && kind != connector.ErrorRetryableAvailability {
		err.WithReceipt(connector.Receipt{
			CallID: call.ID, Provider: "openai", ProviderRequestID: requestID, ObservedAt: time.Now().UTC(),
		})
	}
	return err
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
