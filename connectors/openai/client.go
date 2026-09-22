// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package openai implements the OpenAI Responses API connector.
package openai

import (
	"bytes"
	"context"
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
	Connection       connector.ConnectionRef
	CallID           connector.CallID
	Model            string
	Input            any
	Instructions     string
	StructuredOutput *StructuredOutput
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

func (client *Client) CreateResponse(ctx context.Context, input CreateRequest) (connector.Result[Response], error) {
	if err := input.Connection.Validate(); err != nil {
		return connector.Result[Response]{}, validationError("createResponse", "connection is required", err)
	}
	if err := input.CallID.Validate(); err != nil {
		return connector.Result[Response]{}, validationError("createResponse", "a UUID call ID is required", err)
	}
	if input.Model == "" || input.Input == nil {
		return connector.Result[Response]{}, validationError("createResponse", "model and input are required", nil)
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
	requestID, headers, err := client.do(ctx, http.MethodPost, "/responses", input.Connection, input.CallID, payload, &wire)
	if err != nil {
		var typed *connector.Error
		if errors.As(err, &typed) && typed.Kind != connector.ErrorLocalDefect {
			return connector.Result[Response]{}, typed
		}
		receipt := connector.Receipt{CallID: input.CallID, Provider: "openai", Outcome: connector.ActionUnknown, ObservedAt: time.Now().UTC()}
		return connector.Result[Response]{}, connector.NewError(connector.ErrorUnknownMutation, "openai", "createResponse", "provider outcome is unknown", err).WithReceipt(receipt)
	}
	result := convertResponse(wire)
	receipt := connector.Receipt{
		CallID: input.CallID, Provider: "openai", ProviderObjectID: result.ID,
		ProviderRequestID: requestID, Outcome: connector.ActionSucceeded, ObservedAt: time.Now().UTC(),
	}
	return connector.Result[Response]{Value: result, Receipt: &receipt, Meta: rateLimitMetadata(headers)}, nil
}

func (client *Client) RetrieveResponse(ctx context.Context, connection connector.ConnectionRef, responseID string) (connector.Result[Response], error) {
	if err := connection.Validate(); err != nil || responseID == "" {
		return connector.Result[Response]{}, validationError("retrieveResponse", "connection and response ID are required", err)
	}
	var wire wireResponse
	requestID, headers, err := client.do(ctx, http.MethodGet, "/responses/"+url.PathEscape(responseID), connection, "", nil, &wire)
	if err != nil {
		var typed *connector.Error
		if errors.As(err, &typed) {
			return connector.Result[Response]{}, typed
		}
		return connector.Result[Response]{}, connector.NewError(connector.ErrorRetryableAvailability, "openai", "retrieveResponse", "provider is unavailable", err)
	}
	return connector.Result[Response]{Value: convertResponse(wire), Meta: mergeMetadata(rateLimitMetadata(headers), map[string]string{"providerRequestId": requestID})}, nil
}

func (client *Client) do(ctx context.Context, method, path string, connection connector.ConnectionRef, callID connector.CallID, payload any, output any) (string, http.Header, error) {
	credential, err := client.credentials.Resolve(ctx, connection)
	if err != nil {
		return "", nil, err
	}
	apiKey, err := connector.RequiredCredentialValue(credential, "api_key")
	if err != nil {
		return "", nil, connector.NewError(connector.ErrorAuthentication, "openai", path, "API key is not configured", err)
	}
	var body io.Reader
	if payload != nil {
		encoded, encodeErr := json.Marshal(payload)
		if encodeErr != nil {
			return "", nil, validationError(path, "request is not JSON serializable", encodeErr)
		}
		body = bytes.NewReader(encoded)
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + path
	request, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return "", nil, validationError(path, "build request", err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Content-Type", "application/json")
	if callID != "" {
		request.Header.Set("Idempotency-Key", string(callID))
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return "", nil, err
	}
	defer response.Body.Close()
	requestID := response.Header.Get("X-Request-Id")
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return requestID, response.Header, openAIStatusError(path, response.StatusCode, response.Header, requestID, callID)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if err := decoder.Decode(output); err != nil {
		return requestID, response.Header, connector.NewError(connector.ErrorLocalDefect, "openai", path, "provider returned an invalid response", err)
	}
	return requestID, response.Header, nil
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

func openAIStatusError(operation string, status int, header http.Header, requestID string, callID connector.CallID) error {
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
			kind = connector.ErrorRetryableAvailability
		}
	}
	err := connector.NewError(kind, "openai", operation, "provider returned HTTP "+strconv.Itoa(status), nil)
	if kind == connector.ErrorRateLimit {
		if seconds, parseErr := strconv.Atoi(header.Get("Retry-After")); parseErr == nil {
			err.WithRetryAfter(time.Duration(seconds) * time.Second)
		}
	}
	if callID != "" && kind != connector.ErrorRateLimit && kind != connector.ErrorRetryableAvailability {
		receipt := connector.Receipt{
			CallID: callID, Provider: "openai", ProviderRequestID: requestID,
			Outcome: connector.ActionFailed, ObservedAt: time.Now().UTC(),
		}
		err.WithReceipt(receipt)
	}
	return err
}

func rateLimitMetadata(header http.Header) map[string]string {
	metadata := map[string]string{}
	for _, name := range []string{"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests", "x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens"} {
		if value := header.Get(name); value != "" {
			metadata[name] = value
		}
	}
	return metadata
}

func mergeMetadata(left, right map[string]string) map[string]string {
	for key, value := range right {
		left[key] = value
	}
	return left
}
