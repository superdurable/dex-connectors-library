// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package httpconnector provides bounded HTTP queries and idempotent actions.
package httpconnector

import (
	"bytes"
	"context"
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
	Connection connector.ConnectionRef
	Path       string
	Query      url.Values
	Headers    map[string]string
	Body       any
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

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
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	idempotencyHeader := config.IdempotencyHeader
	if idempotencyHeader == "" {
		idempotencyHeader = "Idempotency-Key"
	}
	return &Client{
		baseURL:           baseURL,
		allowedHosts:      allowedHosts,
		maxResponseBytes:  maxBytes,
		credentialHeaders: config.CredentialHeaders,
		idempotencyHeader: idempotencyHeader,
		httpClient:        client,
		credentials:       credentials,
	}, nil
}

func (client *Client) Query(ctx context.Context, method string, input Request) (connector.Result[Response], error) {
	if method != http.MethodGet && method != http.MethodHead {
		return connector.Result[Response]{}, connector.NewError(connector.ErrorValidation, "http", "query", "query method must be GET or HEAD", nil)
	}
	response, requestID, err := client.do(ctx, method, input, "")
	if err != nil {
		return connector.Result[Response]{}, classifyTransportError(connector.OperationQuery, "query", connector.CallID(""), err)
	}
	if err := statusError("query", response, requestID, ""); err != nil {
		return connector.Result[Response]{}, err
	}
	return connector.Result[Response]{Value: response, Meta: map[string]string{"providerRequestId": requestID}}, nil
}

func (client *Client) Action(ctx context.Context, method string, callID connector.CallID, input Request) (connector.Result[Response], error) {
	if method != http.MethodPost && method != http.MethodPut && method != http.MethodPatch && method != http.MethodDelete {
		return connector.Result[Response]{}, connector.NewError(connector.ErrorValidation, "http", "action", "unsupported action method", nil)
	}
	if err := callID.Validate(); err != nil {
		return connector.Result[Response]{}, connector.NewError(connector.ErrorValidation, "http", "action", "a UUID call ID is required", err)
	}
	response, requestID, err := client.do(ctx, method, input, callID)
	if err != nil {
		return connector.Result[Response]{}, classifyTransportError(connector.OperationAction, "action", callID, err)
	}
	if err := statusError("action", response, requestID, callID); err != nil {
		return connector.Result[Response]{}, err
	}
	receipt := connector.Receipt{
		CallID: callID, Provider: "http", ProviderRequestID: requestID,
		Outcome: connector.ActionSucceeded, ObservedAt: time.Now().UTC(),
	}
	return connector.Result[Response]{Value: response, Receipt: &receipt}, nil
}

func (client *Client) do(ctx context.Context, method string, input Request, callID connector.CallID) (Response, string, error) {
	if err := input.Connection.Validate(); err != nil {
		return Response{}, "", httpValidationError(method, "connection is required", err)
	}
	target, err := client.baseURL.Parse(input.Path)
	if err != nil || !client.allowedHosts[strings.ToLower(target.Hostname())] {
		return Response{}, "", httpValidationError(method, "request target is outside the host allowlist", err)
	}
	target.RawQuery = input.Query.Encode()
	var body io.Reader
	if input.Body != nil {
		encoded, encodeErr := json.Marshal(input.Body)
		if encodeErr != nil {
			return Response{}, "", httpValidationError(method, "request body is not JSON serializable", encodeErr)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return Response{}, "", httpValidationError(method, "request is invalid", err)
	}
	if input.Body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range input.Headers {
		if isSecretHeader(name) {
			return Response{}, "", httpValidationError(method, "secret headers must come from CredentialProvider", nil)
		}
		request.Header.Set(name, value)
	}
	credential, err := client.credentials.Resolve(ctx, input.Connection)
	if err != nil {
		return Response{}, "", err
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
		return Response{}, "", err
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, client.maxResponseBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil {
		return Response{}, "", err
	}
	if int64(len(responseBody)) > client.maxResponseBytes {
		return Response{}, "", connector.NewError(connector.ErrorTerminalRejection, "http", strings.ToLower(method), "provider response exceeds the configured size limit", nil)
	}
	requestID := firstHeader(response.Header, "X-Request-Id", "Request-Id", "Traceparent")
	return Response{StatusCode: response.StatusCode, Header: safeResponseHeaders(response.Header), Body: responseBody}, requestID, nil
}

func httpValidationError(operation, message string, cause error) error {
	return connector.NewError(connector.ErrorValidation, "http", strings.ToLower(operation), message, cause)
}

func classifyTransportError(kind connector.OperationKind, operation string, callID connector.CallID, cause error) error {
	var connectorErr *connector.Error
	if errors.As(cause, &connectorErr) {
		return connectorErr
	}
	if kind == connector.OperationAction {
		receipt := connector.Receipt{CallID: callID, Provider: "http", Outcome: connector.ActionUnknown, ObservedAt: time.Now().UTC()}
		return connector.NewError(connector.ErrorUnknownMutation, "http", operation, "provider outcome is unknown", cause).WithReceipt(receipt)
	}
	return connector.NewError(connector.ErrorRetryableAvailability, "http", operation, "provider is unavailable", cause)
}

func statusError(operation string, response Response, requestID string, callID connector.CallID) error {
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
			kind = connector.ErrorRetryableAvailability
		}
	}
	err := connector.NewError(kind, "http", operation, "provider returned HTTP "+strconv.Itoa(response.StatusCode), nil)
	if kind == connector.ErrorRateLimit {
		if seconds, parseErr := strconv.Atoi(response.Header.Get("Retry-After")); parseErr == nil {
			err.WithRetryAfter(time.Duration(seconds) * time.Second)
		}
	}
	if callID != "" && kind != connector.ErrorRateLimit && kind != connector.ErrorRetryableAvailability {
		receipt := connector.Receipt{
			CallID: callID, Provider: "http", ProviderRequestID: requestID,
			Outcome: connector.ActionFailed, ObservedAt: time.Now().UTC(),
		}
		err.WithReceipt(receipt)
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
		"Content-Type", "ETag", "Last-Modified", "Retry-After",
		"X-Request-Id", "Request-Id", "Traceparent",
		"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset",
	} {
		if values := header.Values(name); len(values) > 0 {
			safe[name] = append([]string(nil), values...)
		}
	}
	return safe
}
