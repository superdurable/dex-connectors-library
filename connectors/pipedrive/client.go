// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package pipedrive connects Dex applications to Pipedrive CRM persons,
// organizations, and deals through Pipedrive API v2.
//
// A Client exposes six operations. SearchObjects, GetObject, and ListObjects
// read records; UpdateObject sets named fields on one record and is safe to
// repeat. CreateObject and UpsertObject create records, and Pipedrive has no
// idempotency key. Both run with sync durability and record a Dex heartbeat
// checkpoint before a create is sent: CreateObject then reports an unconfirmed
// outcome as uncertain instead of sending again, and UpsertObject, which finds
// a person by email or an organization by name first, reads the record back by
// that identity. Dex accepts the checkpoint once the Worker writes it to its
// stream, before storing it, so a Worker lost in that instant can still send
// twice: the checkpoint narrows the double-send window but does not close it.
//
// A connection authenticates with a Personal API token sent in the
// x-api-token header to https://api.pipedrive.com, or with a Pipedrive OAuth
// app whose hourly access token is refreshed through CredentialRefreshDriver
// and whose requests go to the company's api_domain.
package pipedrive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// APITokenAuthMethodID identifies a Personal API token sent in the x-api-token header.
	APITokenAuthMethodID = "api-token"
	// OAuthAuthMethodID identifies a Pipedrive OAuth app with automatic access token refresh.
	OAuthAuthMethodID = "pipedrive-oauth"

	providerName = "pipedrive"
	// defaultRequestTimeout bounds one Pipedrive request when WithHTTPClient supplies no Timeout.
	defaultRequestTimeout = 20 * time.Second
	// operationDeadline keeps every request of one attempt, including a token refresh, inside the 30-second Execute timeout.
	operationDeadline = 25 * time.Second
	// burstRetryDelay is Pipedrive's rolling burst-limit window.
	burstRetryDelay = 2 * time.Second
	// maximumBurstRetryDelay bounds a delay read from x-ratelimit-reset.
	maximumBurstRetryDelay = 10 * time.Second

	apiTokenHeader             = "X-Api-Token"
	correlationIDHeader        = "X-Correlation-Id"
	dailyTokensRemainingHeader = "X-Daily-Ratelimit-Token-Remaining"
	burstResetHeader           = "X-Ratelimit-Reset"
)

var (
	correlationIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	errRequestNotBuilt   = errors.New("Pipedrive request could not be built")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
}

// WithHTTPClient replaces the HTTP client used for Pipedrive API and token
// calls. The Client uses a copy that never follows redirects and, when the
// supplied client has no Timeout, applies a 20-second request timeout. The
// caller keeps ownership of the client and its transport.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithClock replaces the clock used for receipts, Retry-After dates, and OAuth expiry.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// Client executes authenticated Pipedrive API calls. It is safe for concurrent
// use by several Steps.
type Client struct {
	apiTokenBaseURL  string
	httpClient       *http.Client
	credentials      CredentialSource
	refreshDriver    *CredentialRefreshDriver
	maxResponseBytes int64
	now              func() time.Time
}

// New validates configuration and constructs a Pipedrive client. A blank
// Endpoint uses https://api.pipedrive.com for Personal API token connections,
// and a zero MaxResponseBytes uses one MiB. An endpoint must be an HTTPS URL
// on pipedrive.com or one of its subdomains, without a path; a loopback HTTP
// URL is accepted for local tests. OAuth connections ignore Endpoint and use
// the api_domain stored with their credentials. The credential source is
// consulted before every provider call, so token replacement and refresh take
// effect without a restart.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	apiTokenBaseURL, err := validatePipedriveBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Pipedrive endpoint: %w", err)
	}
	if config.MaxResponseBytes <= 0 {
		return nil, errors.New("Pipedrive maxResponseBytes must be positive")
	}
	if credentials == nil {
		return nil, errors.New("Pipedrive credential source is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Pipedrive connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("Pipedrive connector clock is required")
	}
	httpClient := providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout)
	refreshDriver := NewCredentialRefreshDriver(httpClient)
	refreshDriver.now = dependencies.now
	return &Client{
		apiTokenBaseURL: apiTokenBaseURL, httpClient: httpClient, credentials: credentials, refreshDriver: refreshDriver,
		maxResponseBytes: config.MaxResponseBytes, now: dependencies.now,
	}, nil
}

// SearchObjects returns the search Query bound to this client.
func (client *Client) SearchObjects() SearchObjectsOperation {
	return SearchObjectsOperation{client: client}
}

// GetObject returns the record read Query bound to this client.
func (client *Client) GetObject() GetObjectOperation {
	return GetObjectOperation{client: client}
}

// ListObjects returns the bounded list Query bound to this client.
func (client *Client) ListObjects() ListObjectsOperation {
	return ListObjectsOperation{client: client}
}

// UpsertObject returns the identity upsert Mutation bound to this client.
func (client *Client) UpsertObject() UpsertObjectOperation {
	return UpsertObjectOperation{client: client}
}

// CreateObject returns the checkpointed create Mutation bound to this client.
func (client *Client) CreateObject() CreateObjectOperation {
	return CreateObjectOperation{client: client}
}

// UpdateObject returns the field update Mutation bound to this client.
func (client *Client) UpdateObject() UpdateObjectOperation {
	return UpdateObjectOperation{client: client}
}

// providerRequest is one Pipedrive request; path starts with /api/ below the connection's base URL.
type providerRequest struct {
	method string
	path   string
	query  url.Values
	body   []byte
}

type providerResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

// exchangeOutcome is the provider-neutral class of one exchange before an operation maps it to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	// exchangeNotSent is a request that failed before any connection to Pipedrive was obtained.
	exchangeNotSent
	// exchangeRateLimited is a burst-limit 429, which Pipedrive answers before acting on the request.
	exchangeRateLimited
	// exchangeUnavailable is a request that may have reached Pipedrive without a usable answer.
	exchangeUnavailable
	exchangeInvalid
	exchangeNotFound
	exchangeRejected
	exchangeDefect
)

type pipedriveExchange struct {
	outcome    exchangeOutcome
	response   providerResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// operationSession holds one attempt's credentials, base URL, and deadline.
type operationSession struct {
	call        sdkgo.Call
	operation   string
	context     context.Context
	credentials Credentials
	baseURL     string
}

type sessionRoute uint8

const (
	sessionRetry sessionRoute = iota + 1
	sessionRejected
	sessionDefect
)

// sessionFailure is a session that could not start; nothing was sent to Pipedrive.
type sessionFailure struct {
	route   sessionRoute
	failure sdkgo.Failure
}

// startSession resolves credentials, refreshing an expired OAuth token, and the connection's base URL.
func (client *Client) startSession(call sdkgo.Call, operation string) (*operationSession, context.CancelFunc, *sessionFailure) {
	sessionContext, cancel := context.WithTimeout(call.Context, operationDeadline)
	credentials, err := sdkgo.ResolveCredential(sessionContext, client.credentials, call, client.refreshDriver)
	var failure *sessionFailure
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		failure = &sessionFailure{route: sessionRejected, failure: providerFailure(operation, sdkgo.FailureAuthentication, "Pipedrive authorization must be renewed")}
	case errors.Is(err, errCredentialRefreshUnavailable):
		failure = &sessionFailure{route: sessionRetry, failure: providerFailure(operation, sdkgo.FailureAvailability, "Pipedrive OAuth token refresh is temporarily unavailable")}
	case err != nil || validateResolvedCredentials(credentials) != nil:
		failure = &sessionFailure{route: sessionDefect, failure: providerFailure(operation, sdkgo.FailureAuthentication, "Pipedrive connection credentials are unavailable")}
	}
	if failure != nil {
		cancel()
		return nil, nil, failure
	}
	baseURL, err := client.baseURLFor(credentials)
	if err != nil {
		cancel()
		return nil, nil, &sessionFailure{route: sessionDefect, failure: providerFailure(operation, sdkgo.FailureValidation, err.Error())}
	}
	return &operationSession{call: call, operation: operation, context: sessionContext, credentials: credentials, baseURL: baseURL}, cancel, nil
}

// baseURLFor returns the API token endpoint, or the OAuth connection's company api_domain.
func (client *Client) baseURLFor(credentials Credentials) (string, error) {
	if credentials.AuthMethodID != OAuthAuthMethodID {
		return client.apiTokenBaseURL, nil
	}
	baseURL, err := validatePipedriveBaseURL(credentials.APIDomain)
	if err != nil {
		return "", errors.New("Pipedrive OAuth connection has no valid api_domain; authorize the connection again")
	}
	return baseURL, nil
}

// exchange refreshes an OAuth token and resends once after a 401, which Pipedrive answers before acting.
func (client *Client) exchange(session *operationSession, request providerRequest) pipedriveExchange {
	result := client.exchangeOnce(session, request)
	if result.response.statusCode != http.StatusUnauthorized || session.credentials.AuthMethodID != OAuthAuthMethodID {
		return result
	}
	if _, supportsRejection := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !supportsRejection {
		return result
	}
	replacement, err := sdkgo.ResolveCredentialAfterRejection(session.context, client.credentials, session.call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(replacement) != nil || replacement.AuthMethodID != OAuthAuthMethodID {
		return result
	}
	baseURL, err := client.baseURLFor(replacement)
	if err != nil {
		return result
	}
	session.credentials, session.baseURL = replacement, baseURL
	return client.exchangeOnce(session, request)
}

func (client *Client) exchangeOnce(session *operationSession, request providerRequest) pipedriveExchange {
	operation := session.operation
	if session.context.Err() != nil {
		return pipedriveExchange{outcome: exchangeNotSent, failure: providerFailure(operation, sdkgo.FailureTransport, "the operation deadline passed before Pipedrive was contacted; no request was sent")}
	}
	target := session.baseURL + request.path
	if len(request.query) > 0 {
		target += "?" + request.query.Encode()
	}
	var body io.Reader
	if request.body != nil {
		body = bytes.NewReader(request.body)
	}
	observation := &dispatchObservation{}
	httpRequest, err := http.NewRequestWithContext(httptrace.WithClientTrace(session.context, observation.clientTrace()), request.method, target, body)
	if err != nil {
		return pipedriveExchange{outcome: exchangeDefect, failure: providerFailure(operation, sdkgo.FailureLocalDefect, errRequestNotBuilt.Error())}
	}
	credential := requestCredential(session.credentials)
	if session.credentials.AuthMethodID == OAuthAuthMethodID {
		httpRequest.Header.Set("Authorization", "Bearer "+credential)
	} else {
		httpRequest.Header.Set(apiTokenHeader, credential)
	}
	httpRequest.Header.Set("Accept", "application/json")
	if request.body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if !observation.hasObtainedConnection() {
			return pipedriveExchange{outcome: exchangeNotSent, failure: providerFailure(operation, sdkgo.FailureTransport, "Pipedrive could not be reached; no request was sent")}
		}
		return pipedriveExchange{outcome: exchangeUnavailable, failure: providerFailure(operation, sdkgo.FailureTransport, "Pipedrive request failed before a response arrived")}
	}
	return client.readResponse(operation, httpResponse, credential)
}

// readRecord sends one GET for a record and decodes it; a malformed or mismatched record is exchangeInvalid.
func (client *Client) readRecord(session *operationSession, objectType ObjectType, objectID string) (CRMObject, pipedriveExchange) {
	result := client.exchange(session, providerRequest{method: http.MethodGet, path: recordPath(objectType, objectID)})
	return decodeExchangedRecord(session.operation, objectType, objectID, result)
}

// patchRecord sends one PATCH for a record and decodes the record Pipedrive returns.
func (client *Client) patchRecord(session *operationSession, objectType ObjectType, objectID string, body []byte) (CRMObject, pipedriveExchange) {
	result := client.exchange(session, providerRequest{method: http.MethodPatch, path: recordPath(objectType, objectID), body: body})
	return decodeExchangedRecord(session.operation, objectType, objectID, result)
}

// readResponse bounds and classifies one response; a body that echoes the credential is never kept.
func (client *Client) readResponse(operation string, httpResponse *http.Response, credential string) pipedriveExchange {
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	limit := client.maxResponseBytes
	if !isSuccess {
		limit = providerhttp.MaxErrorBodyBytes
	}
	content, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, limit)
	closeErr := httpResponse.Body.Close()
	response := providerResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header}
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge) && isSuccess:
		return pipedriveExchange{outcome: exchangeInvalid, response: response, failure: providerFailure(operation, sdkgo.FailureResponseTooLarge, "Pipedrive response exceeds the configured maxResponseBytes limit")}
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge):
		// An oversized error body is never parsed, so the status alone classifies it.
		content = nil
	case readErr != nil || closeErr != nil:
		return pipedriveExchange{outcome: exchangeUnavailable, response: response, failure: providerFailure(operation, sdkgo.FailureTransport, "Pipedrive response could not be read")}
	}
	if credential != "" && bytes.Contains(content, []byte(credential)) {
		if isSuccess {
			return pipedriveExchange{outcome: exchangeInvalid, response: response, failure: providerFailure(operation, sdkgo.FailureProtocol, "Pipedrive response reflected the connection credential")}
		}
		content = nil
	}
	response.body = content
	if isSuccess {
		return pipedriveExchange{outcome: exchangeSucceeded, response: response}
	}
	return client.classifyErrorResponse(operation, response)
}

// classifyErrorResponse maps a non-2xx status and Pipedrive's rate-limit headers, never its message text.
func (client *Client) classifyErrorResponse(operation string, response providerResponse) pipedriveExchange {
	status := response.statusCode
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
	result := pipedriveExchange{response: response}
	switch {
	case status == http.StatusTooManyRequests && isDailyTokenBudgetExhausted(response.header):
		result.outcome, result.failure = exchangeRejected, providerFailure(operation, sdkgo.FailureQuotaExhausted, "Pipedrive's daily API token budget is exhausted until it resets at midnight in Pipedrive's server time zone")
	case status == http.StatusTooManyRequests:
		result.outcome, result.failure = exchangeRateLimited, providerFailure(operation, sdkgo.FailureRateLimit, "Pipedrive's burst rate limit rejected the request")
		result.retryAfter = max(retryAfter, burstLimitDelay(response.header))
	case status == http.StatusRequestTimeout || (status >= 500 && status != http.StatusNotImplemented):
		result.outcome, result.retryAfter = exchangeUnavailable, retryAfter
		result.failure = providerFailure(operation, sdkgo.FailureAvailability, fmt.Sprintf("Pipedrive is temporarily unavailable (HTTP %d)", status))
	case status == http.StatusUnauthorized:
		result.outcome, result.failure = exchangeRejected, providerFailure(operation, sdkgo.FailureAuthentication, "Pipedrive rejected the API token or access token (HTTP 401)")
	case status == http.StatusPaymentRequired:
		result.outcome, result.failure = exchangeRejected, providerFailure(operation, sdkgo.FailureProviderRejection, "Pipedrive requires a payment or plan change for the company account (HTTP 402)")
	case status == http.StatusForbidden:
		result.outcome, result.failure = exchangeRejected, providerFailure(operation, sdkgo.FailureAuthorization, "Pipedrive denied the request, such as a missing scope or permission, or blocked API traffic (HTTP 403)")
	case status == http.StatusNotFound:
		result.outcome, result.failure = exchangeNotFound, providerFailure(operation, sdkgo.FailureNotFound, "Pipedrive found no such record (HTTP 404)")
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		result.outcome, result.failure = exchangeRejected, providerFailure(operation, sdkgo.FailureValidation, fmt.Sprintf("Pipedrive rejected the request as invalid (HTTP %d)", status))
	case status >= 300 && status < 400:
		result.outcome, result.failure = exchangeRejected, providerFailure(operation, sdkgo.FailureProtocol, fmt.Sprintf("Pipedrive redirected the request (HTTP %d)", status))
	default:
		result.outcome, result.failure = exchangeRejected, providerFailure(operation, sdkgo.FailureProviderRejection, fmt.Sprintf("Pipedrive rejected the request (HTTP %d)", status))
	}
	return result
}

func (client *Client) receipt(call sdkgo.Call, response providerResponse, objectID string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
	if response.header != nil {
		if correlationID := response.header.Get(correlationIDHeader); correlationIDPattern.MatchString(correlationID) {
			receipt.ProviderRequestID = correlationID
		}
	}
	return receipt
}

// isDailyTokenBudgetExhausted reads the company's remaining daily token budget that Pipedrive reports beside a 429.
func isDailyTokenBudgetExhausted(header http.Header) bool {
	remaining, err := strconv.ParseInt(strings.TrimSpace(header.Get(dailyTokensRemainingHeader)), 10, 64)
	return err == nil && remaining <= 0
}

// burstLimitDelay waits for Pipedrive's two-second burst window, or the reported reset, up to ten seconds.
func burstLimitDelay(header http.Header) time.Duration {
	seconds, err := strconv.ParseFloat(strings.TrimSpace(header.Get(burstResetHeader)), 64)
	if err != nil || seconds <= 0 {
		return burstRetryDelay
	}
	return min(max(time.Duration(seconds*float64(time.Second)), burstRetryDelay), maximumBurstRetryDelay)
}

// dispatchObservation records whether a connection was obtained; without one no request byte was sent.
type dispatchObservation struct {
	mutex                sync.Mutex
	isConnectionObtained bool
}

func (observation *dispatchObservation) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{GotConn: observation.recordObtainedConnection}
}

func (observation *dispatchObservation) recordObtainedConnection(httptrace.GotConnInfo) {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.isConnectionObtained = true
}

func (observation *dispatchObservation) hasObtainedConnection() bool {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	return observation.isConnectionObtained
}

// validatePipedriveBaseURL accepts an HTTPS origin on pipedrive.com, or a loopback origin for tests.
func validatePipedriveBaseURL(value string) (string, error) {
	baseURL, err := providerhttp.ValidateBaseURL(value)
	if err != nil {
		return "", errors.New("base URL must be an absolute HTTPS URL")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Path != "" {
		return "", errors.New("base URL must not contain a path")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "pipedrive.com" || strings.HasSuffix(host, ".pipedrive.com") || isLoopbackHost(host) {
		return baseURL, nil
	}
	return "", errors.New("base URL must be a pipedrive.com host")
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func validateResolvedCredentials(credentials Credentials) error {
	if credentials.AuthMethodID != APITokenAuthMethodID && credentials.AuthMethodID != OAuthAuthMethodID {
		return errors.New("Pipedrive authorization method is unknown")
	}
	if !providerhttp.IsHeaderSafeCredential(requestCredential(credentials)) {
		return errors.New("Pipedrive credential is missing or malformed")
	}
	return nil
}

// requestCredential is the secret the selected method sends with every API request.
func requestCredential(credentials Credentials) string {
	if credentials.AuthMethodID == OAuthAuthMethodID {
		return credentials.AccessToken.Reveal()
	}
	return credentials.APIToken.Reveal()
}

func providerFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func providerFailurePointer(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := providerFailure(operation, kind, message)
	return &failure
}
