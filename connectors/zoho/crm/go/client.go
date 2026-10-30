// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package crm implements Zoho CRM record operations as Dex connector Steps: findRecords finds
// records of a module that match typed conditions, getRecord reads one record, listModifiedRecords
// lists records changed since a watermark cursor, listModuleFields reads a module's fields and
// picklist values, upsertRecord creates a record or updates the one that holds its duplicate-check
// value, and updateRecord sets named fields of one record. Every operation is safe to repeat, so
// every Step keeps async durability and retries an unconfirmed outcome.
//
// A connection authorizes with Zoho OAuth in one Zoho data center, chosen by its authorization
// method, such as zoho-eu-oauth. Tokens come from that data center's Zoho Accounts server, and
// every request goes to the api_domain Zoho returned with the token, such as
// https://www.zohoapis.eu, after the connector checks that it is a zohoapis host of the same data
// center. Field names, stages, and other picklist values are Zoho CRM's own, never remapped.
package crm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "zoho-crm"

	// operationDeadline keeps every request of one Invoke, including a token refresh, inside the 30-second Execute timeout.
	operationDeadline = 25 * time.Second
	// defaultRequestTimeout bounds one request, so a request, a refresh, and a resend fit in operationDeadline.
	defaultRequestTimeout = 10 * time.Second

	apiPathPrefix = "/crm/v8"
	// authorizationScheme is the scheme every Zoho CRM API example uses.
	authorizationScheme    = "Zoho-oauthtoken "
	remainingCreditsHeader = "X-API-CREDITS-REMAINING"
)

// Zoho CRM error codes the connector classifies; every other code is reported, never interpreted.
const (
	errorCodeScopeMismatch       = "OAUTH_SCOPE_MISMATCH"
	errorCodeDuplicateData       = "DUPLICATE_DATA"
	errorCodeInvalidData         = "INVALID_DATA"
	errorCodeNoPermission        = "NO_PERMISSION"
	errorCodeAuthorizationFailed = "AUTHORIZATION_FAILED"
	errorCodeInvalidModule       = "INVALID_MODULE"
	errorCodeNotSupported        = "NOT_SUPPORTED"
	errorCodeInvalidURLPattern   = "INVALID_URL_PATTERN"
	errorCodeInvalidRequest      = "INVALID_REQUEST_METHOD"
)

var (
	errorCodePattern      = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	creditCountPattern    = regexp.MustCompile(`^[0-9]{1,12}$`)
	errRequestNotBuilt    = errors.New("Zoho CRM request could not be built")
	requestRejectionCodes = map[string]bool{
		errorCodeScopeMismatch: true, errorCodeNoPermission: true, errorCodeAuthorizationFailed: true, errorCodeInvalidModule: true,
		errorCodeNotSupported: true, errorCodeInvalidURLPattern: true, errorCodeInvalidRequest: true,
	}
	permissionCodes = map[string]bool{errorCodeScopeMismatch: true, errorCodeNoPermission: true, errorCodeAuthorizationFailed: true}
	// operationScopes names the scopes each operation needs, for an OAUTH_SCOPE_MISMATCH explanation.
	operationScopes = map[string]string{
		findRecordsOperation: "ZohoCRM.coql.READ or ZohoCRM.modules.READ", listModifiedRecordsOperation: "ZohoCRM.coql.READ or ZohoCRM.modules.READ",
		getRecordOperation: "ZohoCRM.modules.READ", upsertRecordOperation: "ZohoCRM.modules.CREATE",
		updateRecordOperation: "ZohoCRM.modules.UPDATE", listModuleFieldsOperation: "ZohoCRM.settings.fields.READ",
	}
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	apiBaseURL string
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 10 seconds per Zoho CRM
// request and 8 seconds per token request, for both Zoho CRM and Zoho Accounts. A client Timeout
// replaces both defaults. The caller retains ownership of the client and its transport. Requests
// use copies that never follow redirects, so a token is never replayed to another host.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAPIBaseURL replaces every connection's {api_domain}/crm/v8 for a local Zoho CRM-compatible
// fake. The URL must use HTTPS unless its host is loopback, and it must not carry user
// information, a query, or a fragment. Production connections leave it unset; token refresh still
// goes to the data center's Zoho Accounts server.
func WithAPIBaseURL(baseURL string) Option {
	return func(options *clientOptions) { options.apiBaseURL = baseURL }
}

// Client executes authenticated Zoho CRM API requests for connector operations. A Client is safe
// for concurrent use by several Steps.
type Client struct {
	apiBaseURLOverride string
	httpClient         *http.Client
	credentials        CredentialSource
	refreshDriver      sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes   int64
	now                func() time.Time
}

type crmRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

type crmResponse struct {
	statusCode       int
	header           http.Header
	body             []byte
	remainingCredits string
}

// exchangeOutcome is the provider-neutral meaning of one Zoho CRM request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	// exchangeRetry is a 429, 408, 5xx, or transport failure; every operation is safe to repeat.
	exchangeRetry
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// crmExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type crmExchange struct {
	outcome    exchangeOutcome
	response   crmResponse
	summary    zohoErrorSummary
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// operationSession is one Invoke's resolved credential, Zoho CRM API base URL, and deadline.
type operationSession struct {
	context     context.Context
	call        sdkgo.Call
	credentials Credentials
	apiBaseURL  string
}

// sessionRoute is how an operation continues when its session cannot start.
type sessionRoute uint8

const (
	sessionRetry sessionRoute = iota + 1
	sessionRejected
	sessionDefect
)

// sessionFailure explains why an operation could not start; no request was sent.
type sessionFailure struct {
	route   sessionRoute
	failure sdkgo.Failure
}

// New validates configuration and constructs a Zoho CRM client.
//
// Credentials are resolved, and refreshed when they expire or lack an api_domain, before every
// provider request, so reauthorization takes effect without a restart; the response limit is
// startup configuration.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Zoho CRM response limit must be positive")
	}
	if credentials == nil {
		return nil, errors.New("Zoho CRM credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Zoho CRM connector option is nil")
		}
		option(&dependencies)
	}
	client := &Client{
		httpClient:  providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials: credentials, refreshDriver: NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes: config.MaxResponseBytes, now: time.Now,
	}
	if dependencies.apiBaseURL != "" {
		validated, err := providerhttp.ValidateBaseURL(dependencies.apiBaseURL)
		if err != nil {
			return nil, fmt.Errorf("Zoho CRM API base URL: %w", err)
		}
		client.apiBaseURLOverride = validated
	}
	return client, nil
}

// FindRecords returns the findRecords Query bound to this client.
func (client *Client) FindRecords() FindRecordsOperation { return FindRecordsOperation{client: client} }

// GetRecord returns the getRecord Query bound to this client.
func (client *Client) GetRecord() GetRecordOperation { return GetRecordOperation{client: client} }

// ListModifiedRecords returns the listModifiedRecords Query bound to this client.
func (client *Client) ListModifiedRecords() ListModifiedRecordsOperation {
	return ListModifiedRecordsOperation{client: client}
}

// ListModuleFields returns the listModuleFields Query bound to this client.
func (client *Client) ListModuleFields() ListModuleFieldsOperation {
	return ListModuleFieldsOperation{client: client}
}

// UpsertRecord returns the upsertRecord Mutation bound to this client.
func (client *Client) UpsertRecord() UpsertRecordOperation {
	return UpsertRecordOperation{client: client}
}

// UpdateRecord returns the updateRecord Mutation bound to this client.
func (client *Client) UpdateRecord() UpdateRecordOperation {
	return UpdateRecordOperation{client: client}
}

// startSession resolves the credential and its API base URL under the deadline; callers cancel a returned session.
func (client *Client) startSession(call sdkgo.Call, operation string) (*operationSession, context.CancelFunc, *sessionFailure) {
	operationContext, cancel := context.WithTimeout(call.Context, operationDeadline)
	credentials, err := sdkgo.ResolveCredential(operationContext, client.credentials, call, client.refreshDriver)
	var failure *sessionFailure
	var apiBaseURL string
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		failure = &sessionFailure{route: sessionRejected, failure: crmFailure(sdkgo.FailureAuthentication, operation,
			"Zoho authorization is revoked or expired; reconnect the connection")}
	case err != nil:
		failure = &sessionFailure{route: sessionRetry, failure: crmFailure(sdkgo.FailureAuthentication, operation,
			"Zoho credentials are temporarily unavailable")}
	default:
		apiBaseURL, err = client.apiBaseURLFor(credentials)
		if err != nil {
			failure = &sessionFailure{route: sessionDefect, failure: crmFailure(sdkgo.FailureAuthentication, operation,
				"Zoho CRM connection credentials are missing or invalid: "+err.Error())}
		}
	}
	if failure != nil {
		cancel()
		return nil, nil, failure
	}
	return &operationSession{context: operationContext, call: call, credentials: credentials, apiBaseURL: apiBaseURL}, cancel, nil
}

// apiBaseURLFor checks a credential and returns {api_domain}/crm/v8, or the override for a local fake.
func (client *Client) apiBaseURLFor(credentials Credentials) (string, error) {
	dataCenter, isKnown := DataCenterForAuthMethod(credentials.AuthMethodID)
	if !isKnown {
		return "", errors.New("the authorization method is not a supported data center")
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return "", errors.New("the access token is missing or is not a valid header value")
	}
	if client.apiBaseURLOverride != "" {
		return client.apiBaseURLOverride, nil
	}
	if credentials.APIDomain == "" {
		return dataCenter.ProductionAPIDomain() + apiPathPrefix, nil
	}
	apiDomain, err := dataCenter.ValidateAPIDomain(credentials.APIDomain)
	if err != nil {
		return "", err
	}
	return apiDomain + apiPathPrefix, nil
}

// exchange resends once after a token 401: Zoho CRM rejects an invalid token before acting.
func (client *Client) exchange(session *operationSession, operation string, request crmRequest) crmExchange {
	var payload []byte
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return crmExchange{outcome: exchangeDefect, failure: crmFailure(sdkgo.FailureLocalDefect, operation, errRequestNotBuilt.Error())}
		}
		payload = encoded
	}
	for attempt := 0; ; attempt++ {
		target := session.apiBaseURL + request.path
		if len(request.query) != 0 {
			target += "?" + request.query.Encode()
		}
		result := client.exchangeOnce(session, operation, request.method, target, payload)
		if result.response.statusCode != http.StatusUnauthorized || result.summary.code == errorCodeScopeMismatch || attempt > 0 {
			return result
		}
		if _, supportsRejection := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !supportsRejection {
			return result
		}
		replacement, err := sdkgo.ResolveCredentialAfterRejection(session.context, client.credentials, session.call, client.refreshDriver)
		if err != nil || replacement.AuthMethodID != session.credentials.AuthMethodID {
			return result
		}
		apiBaseURL, err := client.apiBaseURLFor(replacement)
		if err != nil {
			return result
		}
		session.credentials, session.apiBaseURL = replacement, apiBaseURL
	}
}

func (client *Client) exchangeOnce(session *operationSession, operation string, method string, target string, payload []byte) crmExchange {
	if session.context.Err() != nil {
		return crmExchange{outcome: exchangeRetry, failure: crmFailure(sdkgo.FailureTransport, operation, "the operation deadline passed before Zoho CRM answered")}
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	httpRequest, err := http.NewRequestWithContext(session.context, method, target, body)
	if err != nil {
		return crmExchange{outcome: exchangeDefect, failure: crmFailure(sdkgo.FailureLocalDefect, operation, errRequestNotBuilt.Error())}
	}
	accessToken := session.credentials.AccessToken.Reveal()
	httpRequest.Header.Set("Authorization", authorizationScheme+accessToken)
	httpRequest.Header.Set("Accept", "application/json")
	if payload != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return crmExchange{outcome: exchangeRetry, failure: crmFailure(sdkgo.FailureTransport, operation, "Zoho CRM request failed before a response arrived")}
	}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	limit := client.maxResponseBytes
	if !isSuccess {
		limit = providerhttp.MaxErrorBodyBytes
	}
	content, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, limit)
	closeErr := httpResponse.Body.Close()
	response := crmResponse{
		statusCode: httpResponse.StatusCode, header: httpResponse.Header,
		remainingCredits: safeCreditCount(httpResponse.Header.Get(remainingCreditsHeader)),
	}
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge) && isSuccess:
		return crmExchange{outcome: exchangeInvalid, response: response, failure: crmFailure(sdkgo.FailureResponseTooLarge, operation, "Zoho CRM response exceeds the configured maxResponseBytes limit")}
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge):
		// An oversized error body carries no usable code, so the status alone classifies it.
		content = nil
	case readErr != nil || closeErr != nil:
		return crmExchange{outcome: exchangeRetry, response: response, failure: crmFailure(sdkgo.FailureTransport, operation, "Zoho CRM response could not be read")}
	}
	if accessToken != "" && bytes.Contains(content, []byte(accessToken)) {
		if isSuccess {
			return crmExchange{outcome: exchangeInvalid, response: response, failure: crmFailure(sdkgo.FailureProtocol, operation, "Zoho CRM response reflected the connection credential")}
		}
		content = nil
	}
	response.body = content
	if isSuccess {
		return crmExchange{outcome: exchangeSucceeded, response: response}
	}
	return client.classifyFailureStatus(response, operation)
}

// exchangeWrite classifies a record-level error inside a 2xx like the same error in a 400.
func (client *Client) exchangeWrite(session *operationSession, operation string, request crmRequest) (crmExchange, writeRecordResult) {
	result := client.exchange(session, operation, request)
	if result.outcome != exchangeSucceeded {
		return result, writeRecordResult{}
	}
	written, summary, err := decodeWriteRecordResult(result.response.body)
	switch {
	case err != nil:
		return crmExchange{outcome: exchangeInvalid, response: result.response,
			failure: crmFailure(sdkgo.FailureProtocol, operation, "Zoho CRM returned an invalid write result: "+err.Error())}, writeRecordResult{}
	case summary != nil:
		return rejectedExchange(result.response, *summary, operation), writeRecordResult{}
	}
	return result, written
}

// classifyFailureStatus maps a non-2xx response without reading Zoho CRM's message text.
func (client *Client) classifyFailureStatus(response crmResponse, operation string) crmExchange {
	summary := describeZohoError(response.body)
	response.body = nil
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
	status := response.statusCode
	switch {
	case status == http.StatusTooManyRequests:
		return crmExchange{outcome: exchangeRetry, response: response, summary: summary, retryAfter: retryAfter,
			failure: crmFailure(sdkgo.FailureRateLimit, operation, summary.describe("Zoho CRM rate limited the request (HTTP 429)"))}
	case status == http.StatusRequestTimeout || status >= 500:
		return crmExchange{outcome: exchangeRetry, response: response, summary: summary, retryAfter: retryAfter,
			failure: crmFailure(sdkgo.FailureAvailability, operation, summary.describe(fmt.Sprintf("Zoho CRM could not complete the request (HTTP %d)", status)))}
	case status >= 300 && status < 400:
		return crmExchange{outcome: exchangeRejected, response: response, summary: summary,
			failure: crmFailure(sdkgo.FailureProtocol, operation, fmt.Sprintf("Zoho CRM redirected the request (HTTP %d); check that the connection uses the account's data center", status))}
	default:
		return rejectedExchange(response, summary, operation)
	}
}

// rejectedExchange classifies a conclusive rejection, either a 4xx or a record-level error inside a 2xx.
func rejectedExchange(response crmResponse, summary zohoErrorSummary, operation string) crmExchange {
	status := response.statusCode
	var failure sdkgo.Failure
	switch {
	case summary.code == errorCodeScopeMismatch:
		failure = crmFailure(sdkgo.FailureAuthorization, operation, summary.describe(fmt.Sprintf(
			"Zoho CRM refused the request (HTTP %d): the token lacks %s; reconnect and accept every requested scope", status, operationScopes[operation])))
	case status == http.StatusUnauthorized:
		failure = crmFailure(sdkgo.FailureAuthentication, operation, summary.describe("Zoho CRM rejected the access token (HTTP 401); reconnect the connection"))
	case status == http.StatusForbidden || permissionCodes[summary.code]:
		failure = crmFailure(sdkgo.FailureAuthorization, operation, summary.describe(fmt.Sprintf(
			"Zoho CRM refused the request (HTTP %d): the connection's user lacks permission", status)))
	case summary.code == errorCodeDuplicateData:
		failure = crmFailure(sdkgo.FailureConflict, operation, summary.describe(fmt.Sprintf(
			"Zoho CRM found another record with a value of a unique field (HTTP %d)", status)))
	case summary.isRecordIDInvalid():
		failure = crmFailure(sdkgo.FailureNotFound, operation, summary.describe(fmt.Sprintf("Zoho CRM found no record with the ID (HTTP %d)", status)))
	case status == http.StatusNotFound:
		failure = crmFailure(sdkgo.FailureNotFound, operation, summary.describe("Zoho CRM found no such resource (HTTP 404)"))
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity || (status >= 200 && status < 300):
		failure = crmFailure(sdkgo.FailureValidation, operation, summary.describe(fmt.Sprintf("Zoho CRM rejected the request (HTTP %d)", status)))
	default:
		failure = crmFailure(sdkgo.FailureProviderRejection, operation, summary.describe(fmt.Sprintf("Zoho CRM rejected the request (HTTP %d)", status)))
	}
	return crmExchange{outcome: exchangeRejected, response: response, summary: summary, failure: failure}
}

// receipt records the call identity, the Zoho CRM record, and the remaining API credits when Zoho reports them.
func (client *Client) receipt(call sdkgo.Call, response crmResponse, recordID string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: recordID, ObservedAt: client.now().UTC(),
	}
	if response.remainingCredits != "" {
		receipt.Metadata = map[string]string{"remainingCredits": response.remainingCredits}
	}
	return receipt
}

// queryBranches names an operation's branches for the shared Query outcome mapping; notFound may be empty.
type queryBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// queryAttemptForSession maps a session that could not start onto a Query outcome; nothing was sent.
func queryAttemptForSession[OUT any](failure *sessionFailure, branches queryBranches) sdkgo.QueryAttempt[OUT] {
	var zero OUT
	switch failure.route {
	case sessionRetry:
		return sdkgo.NewQueryRetry[OUT](failure.failure, 0)
	case sessionRejected:
		return sdkgo.NewQueryBranch(branches.providerRejected, zero, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewQueryBranch(branches.defect, zero, &failure.failure, sdkgo.Receipt{})
	}
}

// queryAttemptForExchange returns the terminal attempt for every outcome except success.
func queryAttemptForExchange[OUT any](result crmExchange, receipt sdkgo.Receipt, branches queryBranches) (sdkgo.QueryAttempt[OUT], bool) {
	var zero OUT
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.QueryAttempt[OUT]{}, false
	case exchangeRetry:
		return sdkgo.NewQueryRetry[OUT](result.failure, result.retryAfter), true
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(branches.invalidResponse, zero, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewQueryBranch(branches.defect, zero, &result.failure, receipt), true
	}
	if branches.notFound != "" && result.summary.isRecordIDInvalid() {
		return sdkgo.NewQueryBranch(branches.notFound, zero, &result.failure, receipt), true
	}
	return sdkgo.NewQueryBranch(branches.providerRejected, zero, &result.failure, receipt), true
}

// mutationBranches names a Mutation's branches for the shared outcome mapping; notFound may be empty.
type mutationBranches struct {
	notFound         sdkgo.BranchID
	conflict         sdkgo.BranchID
	recordRejected   sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// mutationAttemptForSession maps a session that could not start onto a Mutation outcome; nothing was sent.
func mutationAttemptForSession[OUT any](failure *sessionFailure, branches mutationBranches) sdkgo.MutationAttempt[OUT] {
	var zero OUT
	switch failure.route {
	case sessionRetry:
		return sdkgo.NewMutationRetry[OUT](failure.failure, 0)
	case sessionRejected:
		return sdkgo.NewMutationBranch(branches.providerRejected, zero, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewMutationBranch(branches.defect, zero, &failure.failure, sdkgo.Receipt{})
	}
}

// mutationAttemptForExchange retries every unconfirmed outcome, because both Mutations are safe to repeat.
func mutationAttemptForExchange[OUT any](result crmExchange, receipt sdkgo.Receipt, value OUT, branches mutationBranches) (sdkgo.MutationAttempt[OUT], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRetry:
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeInvalid:
		return sdkgo.NewMutationBranch(branches.invalidResponse, value, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewMutationBranch(branches.defect, value, &result.failure, receipt), true
	}
	status, summary := result.response.statusCode, result.summary
	switch {
	case summary.code == errorCodeDuplicateData:
		return sdkgo.NewMutationBranch(branches.conflict, value, &result.failure, receipt), true
	case branches.notFound != "" && summary.isRecordIDInvalid():
		return sdkgo.NewMutationBranch(branches.notFound, value, &result.failure, receipt), true
	case requestRejectionCodes[summary.code] || (status >= 300 && status != http.StatusBadRequest && status != http.StatusUnprocessableEntity):
		return sdkgo.NewMutationBranch(branches.providerRejected, value, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(branches.recordRejected, value, &result.failure, receipt), true
	}
}

func safeCreditCount(value string) string {
	if creditCountPattern.MatchString(value) {
		return value
	}
	return ""
}

func modulePath(module string) string {
	return "/" + url.PathEscape(module)
}

func recordPath(module string, recordID string) string {
	return modulePath(module) + "/" + url.PathEscape(recordID)
}

func crmFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func crmFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := crmFailure(kind, operation, message)
	return &failure
}

// joinNonEmpty joins the non-empty parts with sep.
func joinNonEmpty(parts []string, sep string) string {
	kept := parts[:0:0]
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, sep)
}
