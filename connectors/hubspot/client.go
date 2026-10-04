// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package hubspot connects Dex applications to HubSpot CRM contacts,
// companies, and deals.
//
// A Client exposes four operations: SearchObjects and GetObject read records,
// and UpsertObject and UpdateObject write them. Both writes are idempotent at
// HubSpot: an upsert is keyed by a unique property value, and an update
// replaces named property values on one record ID. A repeated dispatch of the
// same write, such as a Dex retry or an asynchronous fallback attempt,
// therefore converges on one record with the same values instead of creating a
// duplicate.
//
// A connection authenticates with a private app access token or with HubSpot
// OAuth. OAuth access tokens last 30 minutes; the connection refreshes them
// through CredentialRefreshDriver before they expire and once after HubSpot
// rejects one.
package hubspot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// PrivateAppTokenAuthMethodID identifies a static private app or static-auth app access token.
	PrivateAppTokenAuthMethodID = "private-app-token"
	// OAuthAuthMethodID identifies HubSpot OAuth with automatic access token refresh.
	OAuthAuthMethodID = "hubspot-oauth"

	providerName          = "hubspot"
	crmAPIVersion         = "2026-09"
	defaultRequestTimeout = 25 * time.Second
	// lockedRetryDelay is HubSpot's documented minimum wait after a 423 Locked response.
	lockedRetryDelay = 2 * time.Second
	// maximumRateLimitIntervalDelay bounds a delay derived from X-HubSpot-RateLimit-Interval-Milliseconds.
	maximumRateLimitIntervalDelay = 10 * time.Second
)

var (
	correlationIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	errorCodePattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
	// knownErrorCategories are HubSpot error categories that may appear in a Failure message.
	knownErrorCategories = []string{
		"BAD_REQUEST", "CONFLICT", "EXPIRED_AUTHENTICATION", "INVALID_AUTHENTICATION", "MISSING_SCOPES",
		"OBJECT_ALREADY_EXISTS", "OBJECT_NOT_FOUND", "RATE_LIMITS", "VALIDATION_ERROR",
	}
	errRequestNotBuilt   = errors.New("HubSpot request could not be built")
	errResponseTooLarge  = errors.New("HubSpot response exceeds the configured size limit")
	errResponseMalformed = errors.New("HubSpot returned a malformed response")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
}

// WithHTTPClient replaces the HTTP client used for HubSpot API and token calls.
// The Client uses a copy that never follows redirects and, when the supplied
// client has no Timeout, applies a 25-second request timeout. The caller keeps
// ownership of the client and its transport.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithClock replaces the clock used for provider receipts and OAuth expiry.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// Client executes authenticated HubSpot CRM API calls. It is safe for
// concurrent use by several Steps.
type Client struct {
	baseURL          string
	httpClient       *http.Client
	credentials      CredentialSource
	refreshDriver    *CredentialRefreshDriver
	maxResponseBytes int64
	now              func() time.Time
}

// providerRequest is one HubSpot HTTP request below the CRM API version path.
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

// outcomeDisposition is the provider-neutral class of one exchange before an operation maps it to a branch.
type outcomeDisposition uint8

const (
	outcomeRetry outcomeDisposition = iota + 1
	outcomeNotFound
	outcomeConflict
	outcomeRejected
	outcomeInvalidResponse
	outcomeDefect
)

// providerOutcome is one unsuccessful exchange: a Retry or a terminal branch class.
type providerOutcome struct {
	disposition outcomeDisposition
	failure     sdkgo.Failure
	retryAfter  time.Duration
	receipt     sdkgo.Receipt
}

// operationBranches names the branch each outcome selects for one operation.
type operationBranches struct {
	notFound        sdkgo.BranchID
	conflict        sdkgo.BranchID
	rejected        sdkgo.BranchID
	invalidResponse sdkgo.BranchID
	defect          sdkgo.BranchID
}

type hubspotErrorBody struct {
	Category   string `json:"category"`
	PolicyName string `json:"policyName"`
	Errors     []struct {
		Code string `json:"code"`
	} `json:"errors"`
}

// New validates configuration and constructs an authenticated HubSpot client.
// A blank Endpoint uses https://api.hubapi.com, and a zero MaxResponseBytes uses
// one MiB. The credential provider is consulted before every provider call, so
// token rotation and OAuth refresh take effect without a restart.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("HubSpot endpoint: %w", err)
	}
	if config.MaxResponseBytes <= 0 {
		return nil, errors.New("HubSpot maxResponseBytes must be positive")
	}
	if credentials == nil {
		return nil, errors.New("HubSpot credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("HubSpot connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("HubSpot connector clock is required")
	}
	httpClient := providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout)
	refreshDriver := NewCredentialRefreshDriver(httpClient)
	refreshDriver.now = dependencies.now
	return &Client{
		baseURL: baseURL, httpClient: httpClient, credentials: credentials, refreshDriver: refreshDriver,
		maxResponseBytes: int64(config.MaxResponseBytes), now: dependencies.now,
	}, nil
}

// SearchObjects returns the CRM search Query bound to this client.
func (client *Client) SearchObjects() SearchObjectsOperation {
	return SearchObjectsOperation{client: client}
}

// GetObject returns the record read Query bound to this client.
func (client *Client) GetObject() GetObjectOperation {
	return GetObjectOperation{client: client}
}

// UpsertObject returns the unique-property upsert Mutation bound to this client.
func (client *Client) UpsertObject() UpsertObjectOperation {
	return UpsertObjectOperation{client: client}
}

// UpdateObject returns the record update Mutation bound to this client.
func (client *Client) UpdateObject() UpdateObjectOperation {
	return UpdateObjectOperation{client: client}
}

// exchange sends one request, refreshing and resending once after HubSpot rejects an OAuth token.
func (client *Client) exchange(call sdkgo.Call, operation string, request providerRequest, objectID string) (providerResponse, *providerOutcome) {
	credentials, outcome := client.resolveCredentials(call, operation)
	if outcome != nil {
		return providerResponse{}, outcome
	}
	response, err := client.send(call, credentials, request)
	if err == nil && response.statusCode == http.StatusUnauthorized && client.canRefreshAfterRejection(credentials) {
		replacement, refreshErr := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		if refreshErr == nil && validateResolvedCredentials(replacement) == nil {
			response, err = client.send(call, replacement, request)
		}
	}
	receipt := client.receipt(call, response, objectID)
	switch {
	case errors.Is(err, errRequestNotBuilt):
		return response, client.outcome(outcomeDefect, operation, sdkgo.FailureLocalDefect, "HubSpot request could not be built", receipt)
	case errors.Is(err, errResponseTooLarge):
		return response, client.outcome(outcomeInvalidResponse, operation, sdkgo.FailureResponseTooLarge, errResponseTooLarge.Error(), receipt)
	case err != nil:
		return response, client.outcome(outcomeRetry, operation, sdkgo.FailureTransport, "HubSpot did not return a complete response", receipt)
	case response.statusCode >= 200 && response.statusCode < 300:
		return response, nil
	}
	classified := classifyErrorResponse(operation, response, client.now())
	classified.receipt = receipt
	return response, &classified
}

func (client *Client) send(call sdkgo.Call, credentials Credentials, request providerRequest) (providerResponse, error) {
	target := client.baseURL + "/" + request.path
	if len(request.query) > 0 {
		target += "?" + request.query.Encode()
	}
	var body io.Reader
	if request.body != nil {
		body = bytes.NewReader(request.body)
	}
	httpRequest, err := http.NewRequestWithContext(call.Context, request.method, target, body)
	if err != nil {
		return providerResponse{}, errRequestNotBuilt
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	if request.body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return providerResponse{}, err
	}
	defer httpResponse.Body.Close()
	response := providerResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
	response.body, err = providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return response, errResponseTooLarge
	}
	return response, err
}

// resolveCredentials separates a revoked grant from a transient refresh failure and a broken connection.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *providerOutcome) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return Credentials{}, client.outcome(outcomeRejected, operation, sdkgo.FailureAuthentication, "HubSpot authorization must be renewed", sdkgo.Receipt{})
	case errors.Is(err, errCredentialRefreshUnavailable):
		return Credentials{}, client.outcome(outcomeRetry, operation, sdkgo.FailureAvailability, "HubSpot OAuth token refresh is temporarily unavailable", sdkgo.Receipt{})
	case err != nil || validateResolvedCredentials(credentials) != nil:
		return Credentials{}, client.outcome(outcomeDefect, operation, sdkgo.FailureAuthentication, "HubSpot connection credentials are unavailable", sdkgo.Receipt{})
	}
	return credentials, nil
}

func (client *Client) canRefreshAfterRejection(credentials Credentials) bool {
	if credentials.AuthMethodID != OAuthAuthMethodID {
		return false
	}
	_, supportsRejectionRefresh := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials])
	return supportsRejectionRefresh
}

func (client *Client) outcome(disposition outcomeDisposition, operation string, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) *providerOutcome {
	return &providerOutcome{disposition: disposition, failure: providerFailure(operation, kind, message), receipt: receipt}
}

func (client *Client) receipt(call sdkgo.Call, response providerResponse, objectID string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
	if response.header != nil {
		if correlationID := response.header.Get("X-HubSpot-Correlation-Id"); correlationIDPattern.MatchString(correlationID) {
			receipt.ProviderRequestID = correlationID
		}
	}
	return receipt
}

// classifyErrorResponse maps a non-2xx status and HubSpot's error category without reading message text.
func classifyErrorResponse(operation string, response providerResponse, now time.Time) providerOutcome {
	errorBody := decodeErrorBody(response.body)
	detail := errorBody.safeDetail()
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), now)
	status := response.statusCode
	switch {
	case status == http.StatusTooManyRequests && errorBody.PolicyName == "DAILY":
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureQuotaExhausted, "HubSpot's daily API request limit is exhausted")
	case status == http.StatusTooManyRequests:
		return providerOutcome{
			disposition: outcomeRetry, retryAfter: max(retryAfter, rateLimitIntervalDelay(response.header)),
			failure: providerFailure(operation, sdkgo.FailureRateLimit, "HubSpot rate limited the request"),
		}
	case status == http.StatusLocked:
		return providerOutcome{
			disposition: outcomeRetry, retryAfter: max(retryAfter, lockedRetryDelay),
			failure: providerFailure(operation, sdkgo.FailureRateLimit, "HubSpot locked the records for a high-volume sync"),
		}
	case status == http.StatusRequestTimeout || status == 477 || (status >= 500 && status != http.StatusNotImplemented):
		return providerOutcome{
			disposition: outcomeRetry, retryAfter: retryAfter,
			failure: providerFailure(operation, sdkgo.FailureAvailability, fmt.Sprintf("HubSpot is temporarily unavailable (HTTP %d)", status)),
		}
	case status == http.StatusUnauthorized:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureAuthentication, "HubSpot rejected the access token"+detail)
	case status == http.StatusForbidden && errorBody.Category == "MISSING_SCOPES":
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureAuthorization, "HubSpot access token lacks a required scope"+detail)
	case status == http.StatusForbidden:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureAuthorization, "HubSpot denied access to the record"+detail)
	case status == http.StatusNotFound:
		return terminalOutcome(outcomeNotFound, operation, sdkgo.FailureNotFound, "HubSpot found no such record"+detail)
	case status == http.StatusConflict:
		return terminalOutcome(outcomeConflict, operation, sdkgo.FailureConflict, "HubSpot reported a conflict with another record"+detail)
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity || errorBody.Category == "VALIDATION_ERROR":
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureValidation, "HubSpot rejected the request as invalid"+detail)
	case status >= 300 && status < 400:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureProtocol, fmt.Sprintf("HubSpot redirected the request (HTTP %d)", status))
	default:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureProviderRejection, fmt.Sprintf("HubSpot rejected the request (HTTP %d)", status)+detail)
	}
}

func terminalOutcome(disposition outcomeDisposition, operation string, kind sdkgo.FailureKind, message string) providerOutcome {
	return providerOutcome{disposition: disposition, failure: providerFailure(operation, kind, message)}
}

func decodeErrorBody(body []byte) hubspotErrorBody {
	var decoded hubspotErrorBody
	if len(body) > providerhttp.MaxErrorBodyBytes || json.Unmarshal(body, &decoded) != nil {
		return hubspotErrorBody{}
	}
	return decoded
}

// safeDetail names only an allowlisted category and an uppercase error code, never message text.
func (errorBody hubspotErrorBody) safeDetail() string {
	var tokens []string
	if slices.Contains(knownErrorCategories, errorBody.Category) {
		tokens = append(tokens, errorBody.Category)
	}
	if len(errorBody.Errors) > 0 && errorCodePattern.MatchString(errorBody.Errors[0].Code) && errorBody.Errors[0].Code != errorBody.Category {
		tokens = append(tokens, errorBody.Errors[0].Code)
	}
	if len(tokens) == 0 {
		return ""
	}
	return " (" + strings.Join(tokens, ", ") + ")"
}

func rateLimitIntervalDelay(header http.Header) time.Duration {
	milliseconds, err := strconv.ParseInt(header.Get("X-HubSpot-RateLimit-Interval-Milliseconds"), 10, 64)
	if err != nil || milliseconds <= 0 {
		return 0
	}
	return min(time.Duration(milliseconds)*time.Millisecond, maximumRateLimitIntervalDelay)
}

func validateResolvedCredentials(credentials Credentials) error {
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("HubSpot access token is missing or malformed")
	}
	return nil
}

func providerFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func providerFailurePointer(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := providerFailure(operation, kind, message)
	return &failure
}

// queryAttemptForOutcome maps an unsuccessful exchange to the Query's Retry or branch.
func queryAttemptForOutcome[T any](outcome providerOutcome, branches operationBranches) sdkgo.QueryAttempt[T] {
	var zero T
	if outcome.disposition == outcomeRetry {
		return sdkgo.NewQueryRetry[T](outcome.failure, outcome.retryAfter)
	}
	return sdkgo.NewQueryBranch(branches.branchFor(outcome.disposition), zero, &outcome.failure, outcome.receipt)
}

// mutationAttemptForOutcome maps an unsuccessful exchange to Retry or a branch; idempotent writes never select uncertain.
func mutationAttemptForOutcome[T any](outcome providerOutcome, branches operationBranches) sdkgo.MutationAttempt[T] {
	var zero T
	if outcome.disposition == outcomeRetry {
		return sdkgo.NewMutationRetry[T](outcome.failure, outcome.retryAfter)
	}
	return sdkgo.NewMutationBranch(branches.branchFor(outcome.disposition), zero, &outcome.failure, outcome.receipt)
}

func (branches operationBranches) branchFor(disposition outcomeDisposition) sdkgo.BranchID {
	switch disposition {
	case outcomeNotFound:
		return preferredBranchOr(branches.notFound, branches.rejected)
	case outcomeConflict:
		return preferredBranchOr(branches.conflict, branches.rejected)
	case outcomeRejected:
		return branches.rejected
	case outcomeInvalidResponse:
		return branches.invalidResponse
	default:
		return branches.defect
	}
}

// preferredBranchOr returns preferred, or fallback when the operation declares no preferred branch.
func preferredBranchOr(preferred sdkgo.BranchID, fallback sdkgo.BranchID) sdkgo.BranchID {
	if preferred == "" {
		return fallback
	}
	return preferred
}
