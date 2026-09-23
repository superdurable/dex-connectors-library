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

type IdempotencyKeyFunc func(connector.CallID, Request) connector.IdempotencyKey

type Option func(*clientOptions)

type clientOptions struct {
	httpClient     *http.Client
	idempotencyKey IdempotencyKeyFunc
	webhookReplay  ReplayGuard
	now            func() time.Time
}

func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

func WithIdempotencyKeyFunc(derive IdempotencyKeyFunc) Option {
	return func(options *clientOptions) { options.idempotencyKey = derive }
}

func WithWebhookReplayGuard(guard ReplayGuard) Option {
	return func(options *clientOptions) { options.webhookReplay = guard }
}

func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

type Client struct {
	baseURL           *url.URL
	allowedHosts      map[string]bool
	maxResponseBytes  int64
	credentialHeaders map[string]string
	idempotencyHeader string
	idempotencyKey    IdempotencyKeyFunc
	httpClient        *http.Client
	credentials       connector.CredentialProvider[Credentials]
	webhookReplay     ReplayGuard
	now               func() time.Time
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

func New(config Config, credentials connector.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
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
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("HTTP connector option is nil")
		}
		option(&dependencies)
	}
	httpClient := dependencies.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: config.Timeout}
	}
	for field := range config.CredentialHeaders {
		if field != "api_key" {
			return nil, fmt.Errorf("unsupported credential field %q", field)
		}
	}
	return &Client{
		baseURL: baseURL, allowedHosts: allowedHosts, maxResponseBytes: config.MaxResponseBytes,
		credentialHeaders: config.CredentialHeaders, idempotencyHeader: config.IdempotencyHeader,
		idempotencyKey: dependencies.idempotencyKey, httpClient: httpClient, credentials: credentials,
		webhookReplay: dependencies.webhookReplay, now: dependencies.now,
	}, nil
}

func (client *Client) Query() QueryOperation { return QueryOperation{client: client} }

func (client *Client) Mutation() MutationOperation { return MutationOperation{client: client} }

func (client *Client) VerifyWebhook() VerifyWebhookOperation {
	return VerifyWebhookOperation{client: client}
}

func (QueryOperation) Definition() connector.QueryDefinition { return QueryDefinition }

func (operation QueryOperation) Invoke(call connector.Call, input Request) connector.QueryAttempt[Response] {
	if input.Method != http.MethodGet && input.Method != http.MethodHead {
		failure := queryFailure(connector.FailureValidation, "query method must be GET or HEAD")
		return connector.NewQueryBranch(QueryBranchDefect, Response{}, &failure, connector.Receipt{})
	}
	response, requestID, dispatched, err := operation.client.do(call, input, "")
	if err != nil {
		if !dispatched {
			failure := requestFailure("query", err)
			return connector.NewQueryBranch(queryFailureBranch(failure), Response{}, &failure, connector.Receipt{})
		}
		return connector.NewQueryRetry[Response](queryFailure(connector.FailureAvailability, "provider is unavailable"), 0)
	}
	receipt := responseReceipt(call, requestID)
	if int64(len(response.Body)) > operation.client.maxResponseBytes {
		failure := queryFailure(connector.FailureResponseTooLarge, "provider response exceeds the configured size limit")
		return connector.NewQueryBranch(QueryBranchFailed, Response{}, &failure, receipt)
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return connector.NewQueryBranch(QueryBranchSucceeded, response, nil, receipt)
	}
	failure, retryAfter, retry := classifyStatus("query", response)
	if retry {
		return connector.NewQueryRetry[Response](failure, retryAfter)
	}
	return connector.NewQueryBranch(QueryBranchFailed, Response{}, &failure, receipt)
}

func (MutationOperation) Definition() connector.MutationDefinition { return MutationDefinition }

func (operation MutationOperation) IdempotencyKey(callID connector.CallID, input Request) connector.IdempotencyKey {
	if operation.client.idempotencyKey == nil {
		return connector.IdempotencyKey(callID)
	}
	return operation.client.idempotencyKey(callID, input)
}

func (operation MutationOperation) Invoke(call connector.Call, input Request) connector.MutationAttempt[Response] {
	if input.Method != http.MethodPost && input.Method != http.MethodPut && input.Method != http.MethodPatch && input.Method != http.MethodDelete {
		failure := mutationFailure(connector.FailureValidation, "unsupported mutation method")
		return connector.NewMutationBranch(MutationBranchDefect, Response{}, &failure, connector.Receipt{})
	}
	response, requestID, dispatched, err := operation.client.do(call, input, call.IdempotencyKey)
	receipt := responseReceipt(call, requestID)
	if err != nil {
		if !dispatched {
			failure := requestFailure("mutation", err)
			return connector.NewMutationBranch(mutationFailureBranch(failure), Response{}, &failure, receipt)
		}
		return connector.NewMutationUncertain(Response{}, mutationFailure(connector.FailureTransport, "provider outcome is unknown"), receipt)
	}
	if int64(len(response.Body)) > operation.client.maxResponseBytes {
		return connector.NewMutationUncertain(Response{}, mutationFailure(connector.FailureResponseTooLarge, "provider outcome is unknown"), receipt)
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return connector.NewMutationBranch(MutationBranchSucceeded, response, nil, receipt)
	}
	failure, retryAfter, retry := classifyStatus("mutation", response)
	if retry {
		return connector.NewMutationRetry[Response](failure, retryAfter)
	}
	if response.StatusCode >= 500 {
		return connector.NewMutationUncertain(Response{}, failure, receipt)
	}
	return connector.NewMutationBranch(MutationBranchRejected, Response{}, &failure, receipt)
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
	if err := credential.Validate(); err != nil {
		return Response{}, "", false, &requestError{kind: connector.FailureAuthentication, message: "connection credentials are invalid"}
	}
	for field, header := range client.credentialHeaders {
		if field == "api_key" && credential.APIKey.Reveal() != "" {
			request.Header.Set(header, credential.APIKey.Reveal())
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

func queryFailureBranch(failure connector.Failure) connector.BranchID {
	if failure.Kind == connector.FailureLocalDefect || failure.Kind == connector.FailureValidation {
		return QueryBranchDefect
	}
	return QueryBranchFailed
}

func mutationFailureBranch(failure connector.Failure) connector.BranchID {
	if failure.Kind == connector.FailureLocalDefect || failure.Kind == connector.FailureValidation {
		return MutationBranchDefect
	}
	return MutationBranchRejected
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
