// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package httpconnector provides bounded HTTP queries and idempotent mutations.
package httpconnector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

const (
	defaultMaxResponseBytes = int64(2 << 20)
	defaultTimeout          = 15 * time.Second
)

var (
	queryDefinition    = connector.QueryDefinition{Operation: connector.OperationRef{ConnectorID: "http", OperationID: "query"}}
	mutationDefinition = connector.MutationDefinition{Operation: connector.OperationRef{ConnectorID: "http", OperationID: "mutation"}}
)

type Config struct {
	BaseURL           string
	AllowedHosts      []string
	Timeout           time.Duration
	MaxResponseBytes  int64
	CredentialHeaders map[string]string
	IdempotencyHeader string
	Client            *http.Client
}

type Client struct {
	baseURL           *url.URL
	allowedHosts      map[string]bool
	maxResponseBytes  int64
	credentialHeaders map[string]string
	idempotencyHeader string
	httpClient        *http.Client
	credentials       connector.CredentialProvider
}

type Request struct {
	Method  string
	Path    string
	Query   url.Values
	Headers map[string]string
	Body    any
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type QueryOperation struct{ client *Client }

type MutationOperation struct{ client *Client }

func New(config Config, credentials connector.CredentialProvider) (*Client, error) {
	baseURL, err := url.Parse(config.BaseURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Hostname() == "" {
		return nil, fmt.Errorf("base URL must be absolute: %w", err)
	}
	if baseURL.Scheme != "https" && !isLoopback(baseURL.Hostname()) {
		return nil, fmt.Errorf("non-loopback HTTP base URL must use HTTPS")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	allowedHosts := make(map[string]bool, len(config.AllowedHosts)+1)
	allowedHosts[strings.ToLower(baseURL.Hostname())] = true
	for _, host := range config.AllowedHosts {
		allowedHosts[strings.ToLower(host)] = true
	}
	timeout := config.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	maxBytes := config.MaxResponseBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxResponseBytes
	}
	httpClient := config.Client
	if httpClient == nil {
		httpClient = &http.Client{Timeout: timeout}
	}
	idempotencyHeader := config.IdempotencyHeader
	if idempotencyHeader == "" {
		idempotencyHeader = "Idempotency-Key"
	}
	return &Client{
		baseURL: baseURL, allowedHosts: allowedHosts, maxResponseBytes: maxBytes,
		credentialHeaders: config.CredentialHeaders, idempotencyHeader: idempotencyHeader,
		httpClient: httpClient, credentials: credentials,
	}, nil
}

func (client *Client) Query() QueryOperation { return QueryOperation{client: client} }

func (client *Client) Mutation() MutationOperation { return MutationOperation{client: client} }

func (QueryOperation) Definition() connector.QueryDefinition { return queryDefinition }

func (operation QueryOperation) Invoke(call connector.Call, input Request) (connector.QueryResult[Response], error) {
	if input.Method != http.MethodGet && input.Method != http.MethodHead {
		return connector.QueryResult[Response]{}, validationError("query", "query method must be GET or HEAD", nil)
	}
	response, requestID, dispatched, err := operation.client.do(call, input, "")
	if err != nil {
		if !dispatched {
			return connector.QueryResult[Response]{}, err
		}
		return connector.QueryResult[Response]{}, classifyTransportError(false, "query", call, err)
	}
	if err := statusError(false, "query", response, requestID, call); err != nil {
		return connector.QueryResult[Response]{}, err
	}
	return connector.QueryResult[Response]{
		Value:   response,
		Receipt: connector.Receipt{CallID: call.ID, Provider: "http", ProviderRequestID: requestID, ObservedAt: time.Now().UTC()},
	}, nil
}

func (MutationOperation) Definition() connector.MutationDefinition { return mutationDefinition }

func (operation MutationOperation) Invoke(call connector.Call, input Request) (connector.MutationResult[Response], error) {
	if input.Method != http.MethodPost && input.Method != http.MethodPut && input.Method != http.MethodPatch && input.Method != http.MethodDelete {
		return connector.MutationResult[Response]{}, validationError("mutation", "unsupported mutation method", nil)
	}
	response, requestID, dispatched, err := operation.client.do(call, input, call.ID)
	if err != nil {
		if !dispatched {
			return connector.MutationResult[Response]{}, err
		}
		return connector.MutationResult[Response]{}, classifyTransportError(true, "mutation", call, err)
	}
	if err := statusError(true, "mutation", response, requestID, call); err != nil {
		return connector.MutationResult[Response]{}, err
	}
	return connector.MutationResult[Response]{
		Outcome: connector.MutationSucceeded,
		Value:   response,
		Receipt: connector.Receipt{CallID: call.ID, Provider: "http", ProviderRequestID: requestID, ObservedAt: time.Now().UTC()},
	}, nil
}

func (client *Client) do(call connector.Call, input Request, callID connector.CallID) (Response, string, bool, error) {
	target, err := client.baseURL.Parse(input.Path)
	if err != nil || !client.allowedHosts[strings.ToLower(target.Hostname())] {
		return Response{}, "", false, validationError(strings.ToLower(input.Method), "request target is outside the host allowlist", err)
	}
	target.RawQuery = input.Query.Encode()
	var body io.Reader
	if input.Body != nil {
		encoded, encodeErr := json.Marshal(input.Body)
		if encodeErr != nil {
			return Response{}, "", false, validationError(strings.ToLower(input.Method), "request body is not JSON serializable", encodeErr)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(call.Context, input.Method, target.String(), body)
	if err != nil {
		return Response{}, "", false, validationError(strings.ToLower(input.Method), "request is invalid", err)
	}
	if input.Body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range input.Headers {
		if isSecretHeader(name) {
			return Response{}, "", false, validationError(strings.ToLower(input.Method), "secret headers must come from CredentialProvider", nil)
		}
		request.Header.Set(name, value)
	}
	credential, err := client.credentials.Resolve(call)
	if err != nil {
		return Response{}, "", false, err
	}
	for field, header := range client.credentialHeaders {
		value, ok := credential.Value(field)
		if ok && value != "" {
			request.Header.Set(header, value)
		}
	}
	if callID != "" {
		request.Header.Set(client.idempotencyHeader, string(callID))
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return Response{}, "", true, err
	}
	defer response.Body.Close()
	requestID := firstHeader(response.Header, "X-Request-Id", "Request-Id", "Traceparent")
	limited := io.LimitReader(response.Body, client.maxResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return Response{}, requestID, true, err
	}
	if int64(len(responseBody)) > client.maxResponseBytes {
		if callID != "" {
			receipt := connector.Receipt{CallID: call.ID, Provider: "http", ProviderRequestID: requestID, ObservedAt: time.Now().UTC()}
			return Response{}, requestID, true, connector.NewError(connector.ErrorUnknownMutation, "http", "mutation", "provider outcome is unknown", nil).WithReceipt(receipt)
		}
		return Response{}, requestID, true, connector.NewError(connector.ErrorTerminalRejection, "http", strings.ToLower(input.Method), "provider response exceeds the configured size limit", nil)
	}
	return Response{StatusCode: response.StatusCode, Header: safeResponseHeaders(response.Header), Body: responseBody}, requestID, true, nil
}

func validationError(operation, message string, cause error) error {
	return connector.NewError(connector.ErrorValidation, "http", operation, message, cause)
}

func classifyTransportError(mutation bool, operation string, call connector.Call, cause error) error {
	var connectorErr *connector.Error
	if errors.As(cause, &connectorErr) {
		return connectorErr
	}
	if mutation {
		receipt := connector.Receipt{CallID: call.ID, Provider: "http", ObservedAt: time.Now().UTC()}
		return connector.NewError(connector.ErrorUnknownMutation, "http", operation, "provider outcome is unknown", cause).WithReceipt(receipt)
	}
	return connector.NewError(connector.ErrorRetryableAvailability, "http", operation, "provider is unavailable", cause)
}

func statusError(mutation bool, operation string, response Response, requestID string, call connector.Call) error {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	kind := connector.ErrorTerminalRejection
	switch response.StatusCode {
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
		if response.StatusCode >= 500 {
			if mutation {
				kind = connector.ErrorUnknownMutation
			} else {
				kind = connector.ErrorRetryableAvailability
			}
		}
	}
	err := connector.NewError(kind, "http", operation, "provider returned HTTP "+strconv.Itoa(response.StatusCode), nil)
	if kind == connector.ErrorRateLimit {
		if seconds, parseErr := strconv.Atoi(response.Header.Get("Retry-After")); parseErr == nil {
			err.WithRetryAfter(time.Duration(seconds) * time.Second)
		}
	}
	if mutation && kind != connector.ErrorRateLimit && kind != connector.ErrorRetryableAvailability {
		err.WithReceipt(connector.Receipt{
			CallID: call.ID, Provider: "http", ProviderRequestID: requestID, ObservedAt: time.Now().UTC(),
		})
	}
	return err
}

func isLoopback(host string) bool {
	return host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
}

func isSecretHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "api-key":
		return true
	default:
		return false
	}
}

func firstHeader(header http.Header, names ...string) string {
	for _, name := range names {
		if value := header.Get(name); value != "" {
			return value
		}
	}
	return ""
}

func safeResponseHeaders(header http.Header) http.Header {
	safe := make(http.Header)
	for _, name := range []string{
		"Content-Type", "ETag", "Last-Modified", "Retry-After", "X-Request-Id", "Request-Id",
		"Traceparent", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset",
	} {
		if values := header.Values(name); len(values) > 0 {
			safe[name] = append([]string(nil), values...)
		}
	}
	return safe
}
