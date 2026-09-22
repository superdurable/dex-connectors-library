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

type IdempotencyKeyFunc func(connector.CallID, Request) connector.IdempotencyKey

type Config struct {
	BaseURL           string
	AllowedHosts      []string
	Timeout           time.Duration
	MaxResponseBytes  int64
	CredentialHeaders map[string]string
	IdempotencyHeader string
	IdempotencyKey    IdempotencyKeyFunc
	Client            *http.Client
}

type Client struct {
	baseURL           *url.URL
	allowedHosts      map[string]bool
	maxResponseBytes  int64
	credentialHeaders map[string]string
	idempotencyHeader string
	idempotencyKey    IdempotencyKeyFunc
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

type requestError struct {
	kind    connector.FailureKind
	message string
}

func (err *requestError) Error() string { return err.message }

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
		idempotencyKey: config.IdempotencyKey, httpClient: httpClient, credentials: credentials,
	}, nil
}

func (client *Client) Query() QueryOperation { return QueryOperation{client: client} }

func (client *Client) Mutation() MutationOperation { return MutationOperation{client: client} }

func (QueryOperation) Definition() connector.QueryDefinition { return queryDefinition }

func (operation QueryOperation) Invoke(call connector.Call, input Request) connector.QueryAttempt[Response] {
	if input.Method != http.MethodGet && input.Method != http.MethodHead {
		return connector.NewQueryFailure(Response{}, queryFailure(connector.FailureValidation, "query method must be GET or HEAD"), connector.Receipt{})
	}
	response, requestID, dispatched, err := operation.client.do(call, input, "")
	if err != nil {
		if !dispatched {
			return connector.NewQueryFailure(Response{}, requestFailure("query", err), connector.Receipt{})
		}
		return connector.NewQueryRetry[Response](queryFailure(connector.FailureAvailability, "provider is unavailable"), 0)
	}
	receipt := responseReceipt(call, requestID)
	if int64(len(response.Body)) > operation.client.maxResponseBytes {
		return connector.NewQueryFailure(Response{}, queryFailure(connector.FailureResponseTooLarge, "provider response exceeds the configured size limit"), receipt)
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return connector.NewQuerySuccess(response, receipt)
	}
	failure, retryAfter, retry := classifyStatus("query", response)
	if retry {
		return connector.NewQueryRetry[Response](failure, retryAfter)
	}
	return connector.NewQueryFailure(Response{}, failure, receipt)
}

func (MutationOperation) Definition() connector.MutationDefinition { return mutationDefinition }

func (operation MutationOperation) IdempotencyKey(callID connector.CallID, input Request) connector.IdempotencyKey {
	if operation.client.idempotencyKey == nil {
		return connector.IdempotencyKey(callID)
	}
	return operation.client.idempotencyKey(callID, input)
}

func (operation MutationOperation) Invoke(call connector.Call, input Request) connector.MutationAttempt[Response] {
	if input.Method != http.MethodPost && input.Method != http.MethodPut && input.Method != http.MethodPatch && input.Method != http.MethodDelete {
		return connector.NewMutationFailure(Response{}, mutationFailure(connector.FailureValidation, "unsupported mutation method"), connector.Receipt{})
	}
	response, requestID, dispatched, err := operation.client.do(call, input, call.IdempotencyKey)
	receipt := responseReceipt(call, requestID)
	if err != nil {
		if !dispatched {
			return connector.NewMutationFailure(Response{}, requestFailure("mutation", err), receipt)
		}
		return connector.NewMutationUnknown(Response{}, mutationFailure(connector.FailureTransport, "provider outcome is unknown"), receipt)
	}
	if int64(len(response.Body)) > operation.client.maxResponseBytes {
		return connector.NewMutationUnknown(Response{}, mutationFailure(connector.FailureResponseTooLarge, "provider outcome is unknown"), receipt)
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return connector.NewMutationSuccess(response, receipt)
	}
	failure, retryAfter, retry := classifyStatus("mutation", response)
	if retry {
		return connector.NewMutationRetry[Response](failure, retryAfter)
	}
	if response.StatusCode >= 500 {
		return connector.NewMutationUnknown(Response{}, failure, receipt)
	}
	return connector.NewMutationFailure(Response{}, failure, receipt)
}

func (client *Client) do(call connector.Call, input Request, idempotencyKey connector.IdempotencyKey) (Response, string, bool, error) {
	target, err := client.baseURL.Parse(input.Path)
	if err != nil || !client.allowedHosts[strings.ToLower(target.Hostname())] {
		return Response{}, "", false, &requestError{kind: connector.FailureValidation, message: "request target is outside the host allowlist"}
	}
	target.RawQuery = input.Query.Encode()
	var body io.Reader
	if input.Body != nil {
		encoded, encodeErr := json.Marshal(input.Body)
		if encodeErr != nil {
			return Response{}, "", false, &requestError{kind: connector.FailureValidation, message: "request body is not JSON serializable"}
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(call.Context, input.Method, target.String(), body)
	if err != nil {
		return Response{}, "", false, &requestError{kind: connector.FailureValidation, message: "request is invalid"}
	}
	if input.Body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range input.Headers {
		if isSecretHeader(name) {
			return Response{}, "", false, &requestError{kind: connector.FailureValidation, message: "secret headers must come from CredentialProvider"}
		}
		request.Header.Set(name, value)
	}
	credential, err := client.credentials.Resolve(call)
	if err != nil {
		return Response{}, "", false, &requestError{kind: connector.FailureAuthentication, message: "connection credentials are unavailable"}
	}
	for field, header := range client.credentialHeaders {
		value, ok := credential.Value(field)
		if ok && value != "" {
			request.Header.Set(header, value)
		}
	}
	if idempotencyKey != "" {
		request.Header.Set(client.idempotencyHeader, string(idempotencyKey))
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
	return Response{StatusCode: response.StatusCode, Header: safeResponseHeaders(response.Header), Body: responseBody}, requestID, true, nil
}

func classifyStatus(operation string, response Response) (connector.Failure, time.Duration, bool) {
	kind := connector.FailureProviderRejection
	retry := false
	switch response.StatusCode {
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
		if response.StatusCode >= 500 {
			kind = connector.FailureAvailability
			retry = operation == "query"
		}
	}
	var retryAfter time.Duration
	if kind == connector.FailureRateLimit {
		if seconds, err := strconv.Atoi(response.Header.Get("Retry-After")); err == nil && seconds > 0 {
			retryAfter = time.Duration(seconds) * time.Second
		}
	}
	failure := connector.Failure{
		Kind: kind, Provider: "http", Operation: operation,
		Message: "provider returned HTTP " + strconv.Itoa(response.StatusCode),
	}
	return failure, retryAfter, retry
}

func queryFailure(kind connector.FailureKind, message string) connector.Failure {
	return connector.Failure{Kind: kind, Provider: "http", Operation: "query", Message: message}
}

func mutationFailure(kind connector.FailureKind, message string) connector.Failure {
	return connector.Failure{Kind: kind, Provider: "http", Operation: "mutation", Message: message}
}

func requestFailure(operation string, err error) connector.Failure {
	kind := connector.FailureLocalDefect
	message := "request could not be prepared"
	var classified *requestError
	if errors.As(err, &classified) {
		kind = classified.kind
		message = classified.message
	}
	return connector.Failure{Kind: kind, Provider: "http", Operation: operation, Message: message}
}

func responseReceipt(call connector.Call, requestID string) connector.Receipt {
	return connector.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "http",
		ProviderRequestID: requestID, ObservedAt: time.Now().UTC(),
	}
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
