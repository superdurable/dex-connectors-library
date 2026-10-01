// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package airtable connects Dex applications to Airtable bases through the
// Airtable Web API.
//
// A Client exposes four operations on one table: ListRecords and GetRecord
// read records, UpsertRecords creates or updates records matched by merge
// fields, and UpdateRecords sets fields on records by record ID. Cell values
// keep Airtable's typed JSON form, such as numbers, checkboxes, and arrays of
// linked record IDs, through CellValue.
//
// Airtable has no idempotency key. UpsertRecords is repeat-safe because a
// retried write finds the record the first write created and updates it, and
// UpdateRecords is repeat-safe because it sets absolute values. Airtable does
// not enforce a unique merge key, so two upserts running at the same time
// could both create a record; UpsertRecords therefore uses sync durability,
// which never runs two attempts of one Step at once.
//
// A connection authenticates with an Airtable personal access token. Requests
// to one base are spaced to Airtable's limit of 5 requests per second within
// one process, and a 429 response holds that base back for Airtable's
// documented 30 seconds before Dex retries.
package airtable

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName          = "airtable"
	defaultRequestTimeout = 25 * time.Second
	// iteratorUnavailableErrorType is Airtable's 422 error type for an expired list offset.
	iteratorUnavailableErrorType = "LIST_RECORDS_ITERATOR_NOT_AVAILABLE"
)

var (
	errorTypePattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,63}$`)
	errRequestNotBuilt   = errors.New("Airtable request could not be built")
	errResponseTooLarge  = errors.New("Airtable response exceeds the configured size limit")
	errResponseMalformed = errors.New("Airtable returned a malformed response")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
}

// WithHTTPClient replaces the HTTP client used for Airtable API calls. The
// Client uses a copy that never follows redirects and, when the supplied
// client has no Timeout, applies a 25-second request timeout. The caller keeps
// ownership of the client and its transport.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Airtable Web API calls. It is safe for
// concurrent use by several Steps, which share its per-base request pacing.
type Client struct {
	baseURL          string
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
	pacer            *baseRequestPacer
	now              func() time.Time
}

// providerRequest is one Airtable HTTP request below the API base URL.
type providerRequest struct {
	method string
	path   string
	baseID string
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
	outcomeOffsetExpired
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

// operationBranches names the branch each outcome selects for one operation; a blank branch falls back to rejected.
type operationBranches struct {
	notFound        sdkgo.BranchID
	offsetExpired   sdkgo.BranchID
	rejected        sdkgo.BranchID
	invalidResponse sdkgo.BranchID
	defect          sdkgo.BranchID
}

// New validates configuration and constructs an authenticated Airtable client.
// A blank Endpoint uses https://api.airtable.com, and a zero MaxResponseBytes
// uses 4 MiB. The credential provider is consulted before every provider call,
// so a replaced token takes effect without a restart.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Airtable endpoint: %w", err)
	}
	if config.MaxResponseBytes <= 0 {
		return nil, errors.New("Airtable maxResponseBytes must be positive")
	}
	if credentials == nil {
		return nil, errors.New("Airtable credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Airtable connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("Airtable connector clock is required")
	}
	return &Client{
		baseURL: baseURL, httpClient: providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials: credentials, maxResponseBytes: config.MaxResponseBytes,
		pacer: newBaseRequestPacer(dependencies.now), now: dependencies.now,
	}, nil
}

// ListRecords returns the record list Query bound to this client.
func (client *Client) ListRecords() ListRecordsOperation {
	return ListRecordsOperation{client: client}
}

// GetRecord returns the record read Query bound to this client.
func (client *Client) GetRecord() GetRecordOperation {
	return GetRecordOperation{client: client}
}

// UpsertRecords returns the merge-field upsert Mutation bound to this client.
func (client *Client) UpsertRecords() UpsertRecordsOperation {
	return UpsertRecordsOperation{client: client}
}

// UpdateRecords returns the record update Mutation bound to this client.
func (client *Client) UpdateRecords() UpdateRecordsOperation {
	return UpdateRecordsOperation{client: client}
}

// exchange paces, authenticates, and sends one request, then classifies any non-2xx outcome.
func (client *Client) exchange(call sdkgo.Call, operation string, request providerRequest, objectID string) (providerResponse, *providerOutcome) {
	pacingDelay, err := client.pacer.waitForRequestSlot(call.Context, request.baseID)
	if err != nil {
		return providerResponse{}, client.outcome(outcomeRetry, operation, sdkgo.FailureAvailability, "the Step ended while waiting for an Airtable request slot", sdkgo.Receipt{})
	}
	if pacingDelay > 0 {
		outcome := client.outcome(outcomeRetry, operation, sdkgo.FailureRateLimit, "Airtable requests to this base are paced to its rate limit", sdkgo.Receipt{})
		outcome.retryAfter = pacingDelay
		return providerResponse{}, outcome
	}
	credentials, outcome := client.resolveCredentials(call, operation)
	if outcome != nil {
		return providerResponse{}, outcome
	}
	response, err := client.send(call, credentials, request)
	receipt := client.receipt(call, objectID)
	switch {
	case errors.Is(err, errRequestNotBuilt):
		return response, client.outcome(outcomeDefect, operation, sdkgo.FailureLocalDefect, errRequestNotBuilt.Error(), receipt)
	case errors.Is(err, errResponseTooLarge):
		return response, client.outcome(outcomeInvalidResponse, operation, sdkgo.FailureResponseTooLarge, errResponseTooLarge.Error(), receipt)
	case err != nil:
		return response, client.outcome(outcomeRetry, operation, sdkgo.FailureTransport, "Airtable did not return a complete response", receipt)
	case response.statusCode >= 200 && response.statusCode < 300:
		return response, nil
	}
	if response.statusCode == http.StatusTooManyRequests {
		client.pacer.holdAfterRateLimit(request.baseID, rateLimitCooldown)
	}
	classified := classifyErrorResponse(operation, response, client.now())
	classified.receipt = receipt
	return response, &classified
}

func (client *Client) send(call sdkgo.Call, credentials Credentials, request providerRequest) (providerResponse, error) {
	var body io.Reader
	if request.body != nil {
		body = bytes.NewReader(request.body)
	}
	httpRequest, err := http.NewRequestWithContext(call.Context, request.method, client.baseURL+request.path, body)
	if err != nil {
		return providerResponse{}, errRequestNotBuilt
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+credentials.PersonalAccessToken.Reveal())
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

// resolveCredentials separates a revoked grant from a missing or malformed token.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *providerOutcome) {
	var credentials Credentials
	var err error
	if contextProvider, ok := client.credentials.(sdkgo.ContextCredentialProvider[Credentials]); ok && call.Context != nil {
		credentials, err = contextProvider.ResolveContext(call.Context, call)
	} else {
		credentials, err = client.credentials.Resolve(call)
	}
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return Credentials{}, client.outcome(outcomeRejected, operation, sdkgo.FailureAuthentication, "the Airtable connection must be authorized again", sdkgo.Receipt{})
	case err != nil || !providerhttp.IsHeaderSafeCredential(credentials.PersonalAccessToken.Reveal()):
		return Credentials{}, client.outcome(outcomeDefect, operation, sdkgo.FailureAuthentication, "Airtable connection credentials are unavailable", sdkgo.Receipt{})
	}
	return credentials, nil
}

func (client *Client) outcome(disposition outcomeDisposition, operation string, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) *providerOutcome {
	return &providerOutcome{disposition: disposition, failure: providerFailure(operation, kind, message), receipt: receipt}
}

func (client *Client) receipt(call sdkgo.Call, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
}

// classifyErrorResponse maps a non-2xx status and Airtable's error type without reading message text.
func classifyErrorResponse(operation string, response providerResponse, now time.Time) providerOutcome {
	errorType := readErrorType(response.body)
	detail := ""
	if errorType != "" {
		detail = ", " + errorType
	}
	status := response.statusCode
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), now)
	switch {
	case status == http.StatusTooManyRequests:
		return providerOutcome{
			disposition: outcomeRetry, retryAfter: max(retryAfter, rateLimitCooldown),
			failure: providerFailure(operation, sdkgo.FailureRateLimit, "Airtable rate limited requests to this base"),
		}
	case status == http.StatusRequestTimeout || (status >= 500 && status != http.StatusNotImplemented):
		return providerOutcome{
			disposition: outcomeRetry, retryAfter: retryAfter,
			failure: providerFailure(operation, sdkgo.FailureAvailability, fmt.Sprintf("Airtable is temporarily unavailable (HTTP %d%s)", status, detail)),
		}
	case status == http.StatusUnprocessableEntity && errorType == iteratorUnavailableErrorType:
		return terminalOutcome(outcomeOffsetExpired, operation, sdkgo.FailureNotFound, "Airtable no longer recognizes the list offset ("+iteratorUnavailableErrorType+")")
	case status == http.StatusUnauthorized:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureAuthentication, fmt.Sprintf("Airtable rejected the personal access token (HTTP 401%s)", detail))
	case status == http.StatusForbidden:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureAuthorization, fmt.Sprintf("Airtable denied access: check the token's scopes and bases, or the base, table, or field name (HTTP 403%s)", detail))
	case status == http.StatusNotFound:
		return terminalOutcome(outcomeNotFound, operation, sdkgo.FailureNotFound, fmt.Sprintf("Airtable found no such record or route (HTTP 404%s)", detail))
	case status == http.StatusBadRequest || status == http.StatusRequestEntityTooLarge || status == http.StatusUnprocessableEntity:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureValidation, fmt.Sprintf("Airtable rejected the request as invalid (HTTP %d%s)", status, detail))
	case status >= 300 && status < 400:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureProtocol, fmt.Sprintf("Airtable redirected the request (HTTP %d)", status))
	default:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureProviderRejection, fmt.Sprintf("Airtable rejected the request (HTTP %d%s)", status, detail))
	}
}

// readErrorType returns Airtable's uppercase error type from {"error":{"type":...}} or {"error":"..."}, never its message.
func readErrorType(body []byte) string {
	if len(body) > providerhttp.MaxErrorBodyBytes {
		return ""
	}
	for _, token := range providerhttp.ReadErrorTokens(body, []string{"/error/type", "/error"}) {
		if errorTypePattern.MatchString(token) {
			return token
		}
	}
	return ""
}

func terminalOutcome(disposition outcomeDisposition, operation string, kind sdkgo.FailureKind, message string) providerOutcome {
	return providerOutcome{disposition: disposition, failure: providerFailure(operation, kind, message)}
}

func providerFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func providerFailurePointer(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := providerFailure(operation, kind, message)
	return &failure
}

// decodeResponse decodes a complete 2xx body into destination.
func decodeResponse(body []byte, destination any) error {
	if err := json.Unmarshal(body, destination); err != nil {
		return errResponseMalformed
	}
	return nil
}

// recordPath returns /v0/{baseId}/{tableIdOrName} with each segment escaped.
func recordPath(baseID string, tableIDOrName string) string {
	return "/v0/" + url.PathEscape(baseID) + "/" + url.PathEscape(tableIDOrName)
}

// queryAttemptForOutcome maps an unsuccessful exchange to the Query's Retry or branch.
func queryAttemptForOutcome[T any](outcome providerOutcome, branches operationBranches) sdkgo.QueryAttempt[T] {
	var zero T
	if outcome.disposition == outcomeRetry {
		return sdkgo.NewQueryRetry[T](outcome.failure, outcome.retryAfter)
	}
	return sdkgo.NewQueryBranch(branches.branchFor(outcome.disposition), zero, &outcome.failure, outcome.receipt)
}

// mutationAttemptForOutcome maps an unsuccessful exchange to Retry or a branch; both writes are repeat-safe.
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
	case outcomeOffsetExpired:
		return preferredBranchOr(branches.offsetExpired, branches.rejected)
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
