// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package xero connects Dex applications to the Xero Accounting API at
// https://api.xero.com/api.xro/2.0.
//
// A Client exposes six operations. ListContacts and FindContactByEmail read
// contacts, GetInvoice and ListInvoices read sales invoices and bills, and
// CreateInvoice and RecordPayment write them. Every amount is a Decimal, an
// exact base-10 string that never passes through floating point, and every
// enum keeps Xero's own values, such as ACCREC and AUTHORISED.
//
// Both mutations send an Idempotency-Key header derived from the Dex Call ID,
// so a retried or re-dispatched attempt of one Step execution receives Xero's
// cached response instead of creating a second invoice or payment. Xero keeps a
// key for six minutes, and the mutation retry window is four minutes.
//
// A connection authenticates with a Xero Custom Connection, whose client
// credentials grant CredentialRefreshDriver repeats before each 30-minute
// access token expires, or with a Xero web app's OAuth 2.0 authorization,
// whose rotating refresh token it exchanges. An OAuth connection also sends the
// Xero-Tenant-Id header of the configured organisation, which the connector
// reads from https://api.xero.com/connections when the organisation is given
// by name or left blank.
package xero

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// CustomConnectionAuthMethodID identifies a connection that uses a Xero Custom Connection's client credentials.
	CustomConnectionAuthMethodID = "custom-connection"
	// OAuthAuthMethodID identifies a connection authorized through a Xero web app's OAuth 2.0 consent.
	OAuthAuthMethodID = "xero-oauth"

	// RetryAfterSecondsReceiptKey names the Receipt metadata entry holding Xero's Retry-After seconds
	// on a dailyLimitReached Result, so an application can wait with a durable Timer.
	RetryAfterSecondsReceiptKey = "retryAfterSeconds"
	// RateLimitProblemReceiptKey names the Receipt metadata entry holding Xero's X-Rate-Limit-Problem value.
	RateLimitProblemReceiptKey = "rateLimitProblem"
	// DayLimitRemainingReceiptKey names the Receipt metadata entry holding X-DayLimit-Remaining.
	DayLimitRemainingReceiptKey = "dayLimitRemaining"
	// MinuteLimitRemainingReceiptKey names the Receipt metadata entry holding X-MinLimit-Remaining.
	MinuteLimitRemainingReceiptKey = "minuteLimitRemaining"
	// AppMinuteLimitRemainingReceiptKey names the Receipt metadata entry holding X-AppMinLimit-Remaining.
	AppMinuteLimitRemainingReceiptKey = "appMinuteLimitRemaining"
	// TenantIDReceiptKey names the Receipt metadata entry holding the Xero-Tenant-Id an OAuth call used.
	TenantIDReceiptKey = "tenantId"

	// DefaultPageSize is the page size listContacts and listInvoices use when the input leaves it zero.
	DefaultPageSize = 25
	// MaxPageSize bounds one page so a Result stays small; Xero itself allows up to 1000.
	MaxPageSize = 100
	// MaxFilterValues bounds an ID or number filter list, the batch size Xero recommends.
	MaxFilterValues = 40

	providerName      = "xero"
	apiHost           = "api.xero.com"
	identityHost      = "identity.xero.com"
	accountingBaseURL = "https://" + apiHost + "/api.xro/2.0"
	connectionsURL    = "https://" + apiHost + "/connections"

	// defaultRequestTimeout bounds one Xero request inside the 30-second Execute timeout.
	defaultRequestTimeout = 20 * time.Second
	// maximumMinuteLimitDelay separates Xero's minute and concurrency limits from its daily limit.
	maximumMinuteLimitDelay = 2 * time.Minute
	// maximumIdempotencyKeyBytes is Xero's documented Idempotency-Key limit.
	maximumIdempotencyKeyBytes = 128
	// maximumReportedValidationErrors bounds the count read from a Xero validation error body.
	maximumReportedValidationErrors = 1000

	idempotencyKeyHeader   = "Idempotency-Key"
	tenantIDHeader         = "Xero-Tenant-Id"
	correlationIDHeader    = "Xero-Correlation-Id"
	rateLimitProblemHeader = "X-Rate-Limit-Problem"
	ifModifiedSinceHeader  = "If-Modified-Since"
)

var (
	errRequestNotBuilt       = errors.New("Xero request could not be built")
	errResponseNotJSONObject = errors.New("response is not a JSON object")
	correlationIDPattern     = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	rateLimitCountPattern    = regexp.MustCompile(`^[0-9]{1,10}$`)
	rateLimitProblemValues   = regexp.MustCompile(`^[A-Za-z]{1,32}$`)
	uuidPattern              = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
	// rateLimitRemainingHeaders maps Xero's remaining-call headers to Receipt metadata keys.
	rateLimitRemainingHeaders = map[string]string{
		"X-DayLimit-Remaining":    DayLimitRemainingReceiptKey,
		"X-MinLimit-Remaining":    MinuteLimitRemainingReceiptKey,
		"X-AppMinLimit-Remaining": AppMinuteLimitRemainingReceiptKey,
	}
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient       *http.Client
	localProviderURL string
	now              func() time.Time
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 20 seconds per request.
// The caller retains ownership of the client and its transport. The connector uses a copy
// that never follows redirects, so a token is never replayed to another host. The same
// client carries the access token requests to https://identity.xero.com/connect/token.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithLocalProviderURL sends every request for https://api.xero.com and
// https://identity.xero.com to one local Xero-compatible fake, keeping each request's
// path, for local verification only. The URL must be an http or https URL whose host is
// loopback, without a path, user information, a query, or a fragment, and the transport
// refuses every other host. Production connections leave it unset.
func WithLocalProviderURL(baseURL string) Option {
	return func(options *clientOptions) { options.localProviderURL = baseURL }
}

// WithClock overrides the clock used for Receipts, Retry-After dates, and token expiry.
// Tests use it; production connections leave it unset.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// Client executes authenticated Xero Accounting API requests for connector operations.
// A Client is safe for concurrent use by several Steps.
type Client struct {
	httpClient       *http.Client
	credentials      CredentialSource
	refreshDriver    *CredentialRefreshDriver
	tenantResolver   *organisationTenantResolver
	maxResponseBytes int64
	now              func() time.Time
}

// xeroRequest is one Accounting API request; a mutation carries the Step's idempotency key.
type xeroRequest struct {
	method        string
	path          string
	query         url.Values
	modifiedSince *time.Time
	payload       any
	isMutation    bool
}

// xeroResponse holds the safe parts of one response; body is kept only when it is usable.
type xeroResponse struct {
	statusCode    int
	header        http.Header
	body          []byte
	correlationID string
}

// exchangeOutcome is the provider-neutral meaning of one Xero request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeNotFound
	// exchangeRetry is safe for reads and, under the Step's idempotency key, for both mutations.
	exchangeRetry
	exchangeRejected
	exchangeDailyLimit
	// exchangeInvalid is an unusable 2xx: invalidResponse for a read, uncertain for a mutation.
	exchangeInvalid
	// exchangeServerError is HTTP 500, which Xero caches under an idempotency key: Retry for a read, uncertain for a mutation.
	exchangeServerError
	exchangeDefect
)

// xeroExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type xeroExchange struct {
	outcome    exchangeOutcome
	response   xeroResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
	metadata   map[string]string
}

// xeroErrorSummary holds only machine-readable tokens and a count from a Xero error body.
type xeroErrorSummary struct {
	errorType            string
	errorNumber          string
	detail               string
	validationErrorCount int
}

// New validates configuration and constructs a Xero client. Credentials are resolved before
// every provider call, so a replaced secret or a refreshed token takes effect without a
// restart; the organisation and response limit are startup configuration. Construction
// never contacts Xero.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Xero response limit must be positive")
	}
	organisation := strings.TrimSpace(config.Organisation)
	if err := validateOrganisation(organisation); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, errors.New("Xero credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Xero connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("Xero connector clock is required")
	}
	callerClient := dependencies.httpClient
	if dependencies.localProviderURL != "" {
		routed, err := newLocalProviderHTTPClient(callerClient, dependencies.localProviderURL)
		if err != nil {
			return nil, err
		}
		callerClient = routed
	}
	httpClient := providerhttp.NewProviderHTTPClient(callerClient, defaultRequestTimeout)
	refreshDriver := NewCredentialRefreshDriver(httpClient)
	refreshDriver.now = dependencies.now
	return &Client{
		httpClient: httpClient, credentials: credentials, refreshDriver: refreshDriver,
		tenantResolver:   newOrganisationTenantResolver(organisation, httpClient, config.MaxResponseBytes),
		maxResponseBytes: config.MaxResponseBytes, now: dependencies.now,
	}, nil
}

// ListContacts returns the listContacts Query bound to this client.
func (client *Client) ListContacts() ListContactsOperation {
	return ListContactsOperation{client: client}
}

// FindContactByEmail returns the findContactByEmail Query bound to this client.
func (client *Client) FindContactByEmail() FindContactByEmailOperation {
	return FindContactByEmailOperation{client: client}
}

// GetInvoice returns the getInvoice Query bound to this client.
func (client *Client) GetInvoice() GetInvoiceOperation { return GetInvoiceOperation{client: client} }

// ListInvoices returns the listInvoices Query bound to this client.
func (client *Client) ListInvoices() ListInvoicesOperation {
	return ListInvoicesOperation{client: client}
}

// CreateInvoice returns the createInvoice Mutation bound to this client.
func (client *Client) CreateInvoice() CreateInvoiceOperation {
	return CreateInvoiceOperation{client: client}
}

// RecordPayment returns the recordPayment Mutation bound to this client.
func (client *Client) RecordPayment() RecordPaymentOperation {
	return RecordPaymentOperation{client: client}
}

// exchange sends one request and resends once after a 401, which Xero answers before running anything.
func (client *Client) exchange(call sdkgo.Call, operation string, request xeroRequest) xeroExchange {
	credentials, failedResolution := client.resolveCredentials(call, operation)
	if failedResolution != nil {
		return *failedResolution
	}
	tenant, failedTenant := client.resolveTenant(call, credentials, operation)
	if failedTenant != nil {
		return *failedTenant
	}
	result := client.send(call, credentials, tenant, operation, request)
	if result.response.statusCode == http.StatusUnauthorized && client.canRefreshAfterRejection() {
		replacement, err := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		if err == nil && validateResolvedCredentials(replacement) == nil && replacement.AuthMethodID == credentials.AuthMethodID {
			result = client.send(call, replacement, tenant, operation, request)
		}
	}
	if result.response.statusCode == http.StatusForbidden && tenant.isDerived {
		client.tenantResolver.forgetDerivedTenant(tenant.tenantID)
	}
	if tenant.tenantID != "" {
		result.metadata = withMetadata(result.metadata, TenantIDReceiptKey, tenant.tenantID)
	}
	return result
}

// resolveCredentials separates a revoked grant from a transient token failure and a broken connection.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *xeroExchange) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return Credentials{}, &xeroExchange{outcome: exchangeRejected,
			failure: xeroFailure(sdkgo.FailureAuthentication, operation, "Xero authorization must be renewed: reconnect the app or replace the client secret")}
	case errors.Is(err, errCredentialRefreshUnavailable):
		return Credentials{}, &xeroExchange{outcome: exchangeRetry,
			failure: xeroFailure(sdkgo.FailureAvailability, operation, "Xero access token request is temporarily unavailable")}
	case err != nil || validateResolvedCredentials(credentials) != nil:
		return Credentials{}, &xeroExchange{outcome: exchangeDefect,
			failure: xeroFailure(sdkgo.FailureAuthentication, operation, "Xero connection credentials are unavailable")}
	}
	return credentials, nil
}

// resolveTenant returns the Xero-Tenant-Id an OAuth connection sends; a Custom Connection sends none.
func (client *Client) resolveTenant(call sdkgo.Call, credentials Credentials, operation string) (organisationTenant, *xeroExchange) {
	if credentials.AuthMethodID != OAuthAuthMethodID {
		return organisationTenant{}, nil
	}
	return client.tenantResolver.resolveTenant(call, credentials.AccessToken, operation, client.now())
}

func (client *Client) canRefreshAfterRejection() bool {
	_, supportsRejectionRefresh := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials])
	return supportsRejectionRefresh
}

// send performs one HTTP exchange and classifies it without deciding retry policy.
func (client *Client) send(call sdkgo.Call, credentials Credentials, tenant organisationTenant, operation string, request xeroRequest) xeroExchange {
	httpRequest, err := client.buildRequest(call, credentials, tenant, request)
	if err != nil {
		return xeroExchange{outcome: exchangeDefect, failure: xeroFailure(sdkgo.FailureLocalDefect, operation, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if isConnectionNeverEstablished(err) {
			return xeroExchange{outcome: exchangeRetry, failure: xeroFailure(sdkgo.FailureTransport, operation, "Xero could not be reached; no request was sent")}
		}
		return xeroExchange{outcome: exchangeRetry, failure: xeroFailure(sdkgo.FailureTransport, operation, "Xero request failed before a response arrived")}
	}
	return classifyHTTPResponse(operation, httpResponse, credentials.AccessToken.Reveal(), client.maxResponseBytes, client.now())
}

func (client *Client) buildRequest(call sdkgo.Call, credentials Credentials, tenant organisationTenant, request xeroRequest) (*http.Request, error) {
	var body io.Reader
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return nil, errRequestNotBuilt
		}
		body = bytes.NewReader(encoded)
	}
	target := accountingBaseURL + request.path
	if len(request.query) != 0 {
		target += "?" + request.query.Encode()
	}
	httpRequest, err := http.NewRequestWithContext(call.Context, request.method, target, body)
	if err != nil {
		return nil, errRequestNotBuilt
	}
	httpRequest.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	httpRequest.Header.Set("Accept", "application/json")
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	if tenant.tenantID != "" {
		httpRequest.Header.Set(tenantIDHeader, tenant.tenantID)
	}
	if request.modifiedSince != nil {
		httpRequest.Header.Set(ifModifiedSinceHeader, request.modifiedSince.UTC().Format(modifiedSinceLayout))
	}
	if request.isMutation {
		if call.IdempotencyKey == "" || len(call.IdempotencyKey) > maximumIdempotencyKeyBytes {
			return nil, errors.New("Xero mutation has no usable idempotency key")
		}
		httpRequest.Header.Set(idempotencyKeyHeader, string(call.IdempotencyKey))
	}
	return httpRequest, nil
}

func (client *Client) receipt(call sdkgo.Call, result xeroExchange, objectID string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ProviderRequestID: result.response.correlationID, ObservedAt: client.now().UTC(),
	}
	metadata := rateLimitRemainingMetadata(result.response.header)
	for key, value := range result.metadata {
		metadata = withMetadata(metadata, key, value)
	}
	if len(metadata) != 0 {
		receipt.Metadata = metadata
	}
	return receipt
}

// classifyHTTPResponse reads a bounded body and maps the status; it closes the body.
func classifyHTTPResponse(operation string, httpResponse *http.Response, secret string, maxResponseBytes int64, now time.Time) xeroExchange {
	defer func() {
		// The body is read to its bound below; a close failure cannot change the classified response.
		_ = httpResponse.Body.Close()
	}()
	response := xeroResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
	if correlationID := httpResponse.Header.Get(correlationIDHeader); correlationIDPattern.MatchString(correlationID) {
		response.correlationID = correlationID
	}
	if !isSuccessStatus(httpResponse.StatusCode) {
		// An error body is truncated, not rejected; only its tokens are read.
		body, err := io.ReadAll(io.LimitReader(httpResponse.Body, providerhttp.MaxErrorBodyBytes))
		if err != nil {
			body = nil
		}
		return classifyFailedStatus(operation, response, summarizeXeroError(body, secret), now)
	}
	body, err := providerhttp.ReadBoundedBody(httpResponse.Body, maxResponseBytes)
	switch {
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		return xeroExchange{outcome: exchangeInvalid, response: response,
			failure: xeroFailure(sdkgo.FailureResponseTooLarge, operation, "Xero response exceeds the configured maxResponseBytes limit")}
	case err != nil:
		return xeroExchange{outcome: exchangeRetry, response: response,
			failure: xeroFailure(sdkgo.FailureTransport, operation, "Xero response was interrupted")}
	case secret != "" && bytes.Contains(body, []byte(secret)):
		return xeroExchange{outcome: exchangeInvalid, response: response,
			failure: xeroFailure(sdkgo.FailureProtocol, operation, "Xero response reflected the connection credential")}
	}
	response.body = body
	return xeroExchange{outcome: exchangeSucceeded, response: response}
}

// classifyFailedStatus maps a non-2xx Xero response; Xero's own message text is never read.
func classifyFailedStatus(operation string, response xeroResponse, summary xeroErrorSummary, now time.Time) xeroExchange {
	status := response.statusCode
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), now)
	result := func(outcome exchangeOutcome, kind sdkgo.FailureKind, message string) xeroExchange {
		return xeroExchange{outcome: outcome, response: response, retryAfter: retryAfter,
			failure: xeroFailure(kind, operation, withErrorSummary(fmt.Sprintf("%s (HTTP %d)", message, status), summary))}
	}
	switch {
	case status == http.StatusTooManyRequests:
		return classifyRateLimit(operation, response, summary, retryAfter)
	case status == http.StatusInternalServerError:
		return result(exchangeServerError, sdkgo.FailureAvailability, "Xero failed internally")
	case status == http.StatusNotImplemented:
		return result(exchangeRejected, sdkgo.FailureProviderRejection, "Xero does not implement the request")
	case status == http.StatusRequestTimeout || status >= 500:
		return result(exchangeRetry, sdkgo.FailureAvailability, "Xero is temporarily unavailable")
	case status == http.StatusUnauthorized:
		return result(exchangeRejected, sdkgo.FailureAuthentication, "Xero rejected the access token")
	case status == http.StatusForbidden:
		return result(exchangeRejected, sdkgo.FailureAuthorization, "Xero denied access to the organisation or scope")
	case status == http.StatusNotFound:
		return result(exchangeNotFound, sdkgo.FailureNotFound, "Xero found no such resource")
	case status == http.StatusBadRequest:
		return result(exchangeRejected, sdkgo.FailureValidation, "Xero rejected the request")
	case status >= 300 && status < 400:
		return result(exchangeRejected, sdkgo.FailureProtocol, "Xero redirected the request")
	default:
		return result(exchangeRejected, sdkgo.FailureProviderRejection, "Xero rejected the request")
	}
}

// classifyRateLimit retries the minute and concurrency limits and reports the daily limit, which resets hours later.
func classifyRateLimit(operation string, response xeroResponse, summary xeroErrorSummary, retryAfter time.Duration) xeroExchange {
	problem := strings.TrimSpace(response.header.Get(rateLimitProblemHeader))
	metadata := map[string]string{}
	if rateLimitProblemValues.MatchString(problem) {
		metadata[RateLimitProblemReceiptKey] = strings.ToLower(problem)
	} else {
		problem = ""
	}
	retryAfterSeconds, hasRetryAfterSeconds := parseRetryAfterSeconds(response.header.Get("Retry-After"))
	isDailyLimit := strings.EqualFold(problem, "day") ||
		(hasRetryAfterSeconds && time.Duration(retryAfterSeconds)*time.Second > maximumMinuteLimitDelay)
	if isDailyLimit {
		if hasRetryAfterSeconds {
			metadata[RetryAfterSecondsReceiptKey] = strconv.FormatInt(retryAfterSeconds, 10)
		}
		return xeroExchange{outcome: exchangeDailyLimit, response: response, metadata: metadata,
			failure: xeroFailure(sdkgo.FailureQuotaExhausted, operation, withErrorSummary("Xero's daily API limit for the organisation is exhausted (HTTP 429)", summary))}
	}
	limit := "rate"
	if problem != "" {
		limit = strings.ToLower(problem)
	}
	return xeroExchange{outcome: exchangeRetry, response: response, retryAfter: retryAfter, metadata: metadata,
		failure: xeroFailure(sdkgo.FailureRateLimit, operation, withErrorSummary(fmt.Sprintf("Xero's %s limit throttled the request (HTTP 429)", limit), summary))}
}

// summarizeXeroError reads the exception type, error number, problem detail, and validation count.
func summarizeXeroError(body []byte, secret string) xeroErrorSummary {
	summary := xeroErrorSummary{}
	if tokens := providerhttp.ReadErrorTokens(body, []string{"/Type"}); len(tokens) == 1 && isCredentialFreeToken(tokens[0], secret) {
		summary.errorType = tokens[0]
	}
	if tokens := providerhttp.ReadErrorTokens(body, []string{"/ErrorNumber"}); len(tokens) == 1 && rateLimitCountPattern.MatchString(tokens[0]) {
		summary.errorNumber = tokens[0]
	}
	if tokens := providerhttp.ReadErrorTokens(body, []string{"/Detail", "/detail", "/error"}); len(tokens) != 0 && isCredentialFreeToken(tokens[0], secret) {
		summary.detail = tokens[0]
	}
	var document struct {
		Elements []struct {
			ValidationErrors []json.RawMessage `json:"ValidationErrors"`
		} `json:"Elements"`
	}
	if json.Unmarshal(body, &document) == nil {
		for _, element := range document.Elements {
			summary.validationErrorCount = min(summary.validationErrorCount+len(element.ValidationErrors), maximumReportedValidationErrors)
		}
	}
	return summary
}

func withErrorSummary(message string, summary xeroErrorSummary) string {
	var parts []string
	if summary.errorType != "" {
		parts = append(parts, summary.errorType)
	}
	if summary.errorNumber != "" {
		parts = append(parts, "error "+summary.errorNumber)
	}
	if summary.detail != "" {
		parts = append(parts, summary.detail)
	}
	switch summary.validationErrorCount {
	case 0:
	case 1:
		parts = append(parts, "1 validation error")
	default:
		parts = append(parts, strconv.Itoa(summary.validationErrorCount)+" validation errors")
	}
	if len(parts) == 0 {
		return message
	}
	return message + " [" + strings.Join(parts, "; ") + "]"
}

// rateLimitRemainingMetadata copies Xero's remaining-call counts, which help an application pace itself.
func rateLimitRemainingMetadata(header http.Header) map[string]string {
	var metadata map[string]string
	for headerName, metadataKey := range rateLimitRemainingHeaders {
		if value := strings.TrimSpace(header.Get(headerName)); rateLimitCountPattern.MatchString(value) {
			metadata = withMetadata(metadata, metadataKey, value)
		}
	}
	return metadata
}

// parseRetryAfterSeconds reads delay-seconds without the one-hour retry cap, for the daily-limit Receipt.
func parseRetryAfterSeconds(value string) (int64, bool) {
	trimmed := strings.TrimSpace(value)
	if !rateLimitCountPattern.MatchString(trimmed) {
		return 0, false
	}
	seconds, err := strconv.ParseInt(trimmed, 10, 64)
	return seconds, err == nil
}

func withMetadata(metadata map[string]string, key string, value string) map[string]string {
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata[key] = value
	return metadata
}

// queryBranches names an operation's branches for the shared read outcome mapping.
type queryBranches struct {
	notFound          sdkgo.BranchID
	providerRejected  sdkgo.BranchID
	dailyLimitReached sdkgo.BranchID
	invalidResponse   sdkgo.BranchID
	defect            sdkgo.BranchID
}

// queryAttemptForExchange maps every non-success outcome; requested is the value those branches carry.
func queryAttemptForExchange[OUT any](result xeroExchange, requested OUT, receipt sdkgo.Receipt, branches queryBranches) (sdkgo.QueryAttempt[OUT], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.QueryAttempt[OUT]{}, false
	case exchangeRetry, exchangeServerError:
		return sdkgo.NewQueryRetry[OUT](result.failure, result.retryAfter), true
	case exchangeNotFound:
		if branches.notFound != "" {
			return sdkgo.NewQueryBranch(branches.notFound, requested, &result.failure, receipt), true
		}
		return sdkgo.NewQueryBranch(branches.providerRejected, requested, &result.failure, receipt), true
	case exchangeDailyLimit:
		return sdkgo.NewQueryBranch(branches.dailyLimitReached, requested, &result.failure, receipt), true
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(branches.invalidResponse, requested, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewQueryBranch(branches.defect, requested, &result.failure, receipt), true
	default:
		return sdkgo.NewQueryBranch(branches.providerRejected, requested, &result.failure, receipt), true
	}
}

// mutationBranches names an operation's branches for the shared write outcome mapping.
type mutationBranches struct {
	providerRejected  sdkgo.BranchID
	dailyLimitReached sdkgo.BranchID
	defect            sdkgo.BranchID
}

// mutationAttemptForExchange retries what the key replays and reports a cached 500 or unusable answer as uncertain.
func mutationAttemptForExchange[OUT any](result xeroExchange, requested OUT, receipt sdkgo.Receipt, branches mutationBranches) (sdkgo.MutationAttempt[OUT], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRetry:
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeServerError, exchangeInvalid:
		return sdkgo.NewMutationUncertain(requested, result.failure, receipt), true
	case exchangeDailyLimit:
		return sdkgo.NewMutationBranch(branches.dailyLimitReached, requested, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewMutationBranch(branches.defect, requested, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(branches.providerRejected, requested, &result.failure, receipt), true
	}
}

// decodeSingleResource reads the one element of a Xero collection envelope such as {"Invoices":[...]}.
func decodeSingleResource[W any](body []byte, collection string) (W, error) {
	var zero W
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return zero, errResponseNotJSONObject
	}
	var elements []W
	contents, isPresent := envelope[collection]
	if !isPresent || json.Unmarshal(contents, &elements) != nil {
		return zero, fmt.Errorf("response has no %s list", collection)
	}
	if len(elements) != 1 {
		return zero, fmt.Errorf("response has %d %s instead of one", len(elements), collection)
	}
	return elements[0], nil
}

// pageWire is Xero's pagination object on a paged collection.
type pageWire struct {
	Page      int `json:"page"`
	PageSize  int `json:"pageSize"`
	PageCount int `json:"pageCount"`
	ItemCount int `json:"itemCount"`
}

// pageBounds is the requested page and size of a bounded list.
type pageBounds struct {
	page     int
	pageSize int
}

// resolvePageBounds applies the defaults and limits shared by listContacts and listInvoices.
func resolvePageBounds(page int, pageSize int) (pageBounds, error) {
	bounds := pageBounds{page: page, pageSize: pageSize}
	if bounds.page == 0 {
		bounds.page = 1
	}
	if bounds.pageSize == 0 {
		bounds.pageSize = DefaultPageSize
	}
	switch {
	case bounds.page < 1:
		return pageBounds{}, errors.New("page must be 1 or greater")
	case bounds.pageSize < 1 || bounds.pageSize > MaxPageSize:
		return pageBounds{}, fmt.Errorf("pageSize must be between 1 and %d", MaxPageSize)
	}
	return bounds, nil
}

// hasMorePages prefers Xero's pagination object and otherwise treats a full page as possibly followed by more.
func (bounds pageBounds) hasMorePages(pagination *pageWire, returned int) bool {
	if pagination != nil && pagination.PageCount > 0 {
		return bounds.page < pagination.PageCount
	}
	return returned >= bounds.pageSize
}

// validateIDList checks a bounded list of Xero UUIDs for a comma-separated filter.
func validateIDList(fieldName string, values []string) error {
	if len(values) > MaxFilterValues {
		return fmt.Errorf("%s can list at most %d IDs", fieldName, MaxFilterValues)
	}
	for _, value := range values {
		if !uuidPattern.MatchString(value) {
			return fmt.Errorf("%s must contain Xero IDs, which are UUIDs such as 4f2a3169-8454-4012-a642-05a88ef32982", fieldName)
		}
	}
	return nil
}

// validateSingleLineText checks optional text that Xero stores on one line, such as a reference.
func validateSingleLineText(fieldName string, value string, maximumCharacters int) error {
	switch {
	case !utf8.ValidString(value):
		return fmt.Errorf("%s must be valid UTF-8", fieldName)
	case utf8.RuneCountInString(value) > maximumCharacters:
		return fmt.Errorf("%s can be at most %d characters", fieldName, maximumCharacters)
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f {
			return fmt.Errorf("%s must be one line without control characters", fieldName)
		}
	}
	return nil
}

// validateWhereLiteral checks a value placed inside double quotes in a Xero where filter.
func validateWhereLiteral(fieldName string, value string, maximumCharacters int) error {
	if err := validateSingleLineText(fieldName, value, maximumCharacters); err != nil {
		return err
	}
	if strings.ContainsAny(value, `"\`) {
		return fmt.Errorf("%s cannot contain a double quote or a backslash", fieldName)
	}
	return nil
}

// isCredentialFreeToken rejects an error token that echoes the access token back.
func isCredentialFreeToken(token string, secret string) bool {
	return secret == "" || !strings.Contains(token, secret)
}

// isConnectionNeverEstablished reports a dial failure, after which Xero cannot have received the request.
func isConnectionNeverEstablished(err error) bool {
	var operationError *net.OpError
	return errors.As(err, &operationError) && operationError.Op == "dial"
}

func isSuccessStatus(status int) bool { return status >= 200 && status < 300 }

func xeroFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func xeroFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := xeroFailure(kind, operation, message)
	return &failure
}
