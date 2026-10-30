// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package desk implements Zoho Desk ticket operations as Dex connector Steps: searchTickets and
// getTicket read tickets, updateTicket changes a ticket's status, priority, assignee, or department
// so that a repeated attempt writes nothing twice, and createTicket and addComment create a ticket
// or add one comment, sending each request at most once per Step execution because Zoho Desk
// documents no idempotency key.
//
// A connection authorizes with Zoho OAuth in one Zoho data center, chosen by its authorization
// method, such as zoho-eu-oauth. Tokens come from that data center's Zoho Accounts server, and
// every request goes to its Zoho Desk host, such as https://desk.zoho.eu/api/v1, with the
// configured orgId header. Statuses and priorities are Zoho Desk's own names, including an
// organization's custom statuses, never remapped to another vocabulary.
package desk

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "zoho-desk"

	// operationDeadline keeps every request of one Invoke, including a token refresh, inside the 30-second Execute timeout.
	operationDeadline = 25 * time.Second
	// defaultRequestTimeout bounds one request, so a request, a refresh, and a resend fit in operationDeadline.
	defaultRequestTimeout = 10 * time.Second

	apiPathPrefix = "/api/v1"
	orgIDHeader   = "orgId"
	// authorizationScheme is the scheme every Zoho Desk API example uses; Zoho also accepts Bearer.
	authorizationScheme    = "Zoho-oauthtoken "
	requestCreditsHeader   = "X-Rate-Limit-Request-Weight-v3"
	remainingCreditsHeader = "X-Rate-Limit-Remaining-v3"

	maximumReportedErrorDetails = 5
)

// Zoho Desk errorCode values the connector explains in a Failure.
const (
	errorCodeOrganizationMismatch = "OAUTH_ORG_MISMATCH"
	errorCodeScopeMismatch        = "SCOPE_MISMATCH"
)

var (
	zohoIDPattern         = regexp.MustCompile(`^[0-9]{1,20}$`)
	errorCodePattern      = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	errorFieldNamePattern = regexp.MustCompile(`^/[A-Za-z][A-Za-z0-9_]{0,63}(?:/[A-Za-z0-9_]{1,64}){0,3}$`)
	errorTypePattern      = regexp.MustCompile(`^[a-z][A-Za-z_]{0,31}$`)
	creditCountPattern    = regexp.MustCompile(`^[0-9]{1,12}$`)
	errRequestNotBuilt    = errors.New("Zoho Desk request could not be built")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	apiBaseURL string
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 10 seconds per Zoho Desk
// request and 8 seconds per token request, for both Zoho Desk and Zoho Accounts. A client Timeout
// replaces both defaults. The caller retains ownership of the client and its transport. Requests
// use copies that never follow redirects, so a token is never replayed to another host.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAPIBaseURL replaces every data center's https://desk.zoho.<domain>/api/v1 for a local Zoho
// Desk-compatible fake. The URL must use HTTPS unless its host is loopback, and it must not carry
// user information, a query, or a fragment. Production connections leave it unset; token refresh
// still goes to the data center's Zoho Accounts server.
func WithAPIBaseURL(baseURL string) Option {
	return func(options *clientOptions) { options.apiBaseURL = baseURL }
}

// Client executes authenticated Zoho Desk API requests for connector operations. A Client is
// safe for concurrent use by several Steps.
type Client struct {
	organizationID     string
	apiBaseURLOverride string
	httpClient         *http.Client
	credentials        CredentialSource
	refreshDriver      sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes   int64
	now                func() time.Time
}

type deskRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

type deskResponse struct {
	statusCode       int
	header           http.Header
	body             []byte
	requestCredits   string
	remainingCredits string
}

// exchangeOutcome is the provider-neutral meaning of one Zoho Desk request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeNotFound
	// exchangeRateLimited is a 429, which Zoho Desk returns for exhausted credits or concurrency instead of processing.
	exchangeRateLimited
	// exchangeNotSent is a connection that failed before any request byte reached Zoho Desk.
	exchangeNotSent
	// exchangeUnavailable is a 5xx, a 408, or a transport failure after connecting; a write may have been applied.
	exchangeUnavailable
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// deskExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type deskExchange struct {
	outcome    exchangeOutcome
	response   deskResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// operationSession is one Invoke's resolved credential, Zoho Desk host, and deadline.
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

// dispatchObservation records whether a request could have reached Zoho Desk.
type dispatchObservation struct {
	mutex                 sync.Mutex
	hasObtainedConnection bool
	hasFailedToConnect    bool
}

// deskErrorSummary holds only Zoho Desk's machine-readable errorCode and fieldName=errorType pairs.
type deskErrorSummary struct {
	errorCode   string
	fieldErrors []string
}

// New validates configuration and constructs a Zoho Desk client.
//
// orgId is required, because Zoho Desk scopes every ticket request to one organization; pick it
// with the organizationPicker unit after Connect. Credentials are resolved, and refreshed when
// they expire, before every provider request, so reauthorization takes effect without a restart;
// orgId and the response limit are startup configuration.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	organizationID := strings.TrimSpace(config.OrgID)
	if organizationID == "" {
		return nil, errors.New("Zoho Desk orgId is required; after Connect, choose the organization with the Zoho Desk organization picker in Dex Web Connections")
	}
	if !zohoIDPattern.MatchString(organizationID) {
		return nil, errors.New("Zoho Desk orgId must be the organization's numeric ID, such as 2389290")
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Zoho Desk response limit must be positive")
	}
	if credentials == nil {
		return nil, errors.New("Zoho Desk credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Zoho Desk connector option is nil")
		}
		option(&dependencies)
	}
	client := &Client{
		organizationID: organizationID,
		httpClient:     providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials:    credentials, refreshDriver: NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes: config.MaxResponseBytes, now: time.Now,
	}
	if dependencies.apiBaseURL != "" {
		validated, err := providerhttp.ValidateBaseURL(dependencies.apiBaseURL)
		if err != nil {
			return nil, fmt.Errorf("Zoho Desk API base URL: %w", err)
		}
		client.apiBaseURLOverride = validated
	}
	return client, nil
}

// SearchTickets returns the searchTickets Query bound to this client.
func (client *Client) SearchTickets() SearchTicketsOperation {
	return SearchTicketsOperation{client: client}
}

// GetTicket returns the getTicket Query bound to this client.
func (client *Client) GetTicket() GetTicketOperation { return GetTicketOperation{client: client} }

// CreateTicket returns the createTicket Mutation bound to this client.
func (client *Client) CreateTicket() CreateTicketOperation {
	return CreateTicketOperation{client: client}
}

// UpdateTicket returns the updateTicket Mutation bound to this client.
func (client *Client) UpdateTicket() UpdateTicketOperation {
	return UpdateTicketOperation{client: client}
}

// AddComment returns the addComment Mutation bound to this client.
func (client *Client) AddComment() AddCommentOperation { return AddCommentOperation{client: client} }

// startSession starts the operation deadline, then resolves, and refreshes when due, the credential
// and its data center's host. The caller must call cancel when the session is returned.
func (client *Client) startSession(call sdkgo.Call, operation string) (*operationSession, context.CancelFunc, *sessionFailure) {
	operationContext, cancel := context.WithTimeout(call.Context, operationDeadline)
	credentials, err := sdkgo.ResolveCredential(operationContext, client.credentials, call, client.refreshDriver)
	var failure *sessionFailure
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		failure = &sessionFailure{route: sessionRejected, failure: deskFailure(sdkgo.FailureAuthentication, operation,
			"Zoho authorization is revoked or expired; reconnect the connection")}
	case err != nil:
		failure = &sessionFailure{route: sessionRetry, failure: deskFailure(sdkgo.FailureAuthentication, operation,
			"Zoho credentials are temporarily unavailable")}
	case validateResolvedCredentials(credentials) != nil:
		failure = &sessionFailure{route: sessionDefect, failure: deskFailure(sdkgo.FailureAuthentication, operation,
			"Zoho Desk connection credentials are missing or invalid")}
	}
	if failure != nil {
		cancel()
		return nil, nil, failure
	}
	apiBaseURL := client.apiBaseURLOverride
	if apiBaseURL == "" {
		dataCenter, _ := DataCenterForAuthMethod(credentials.AuthMethodID)
		apiBaseURL = dataCenter.DeskURL + apiPathPrefix
	}
	return &operationSession{context: operationContext, call: call, credentials: credentials, apiBaseURL: apiBaseURL}, cancel, nil
}

// queryAttemptForSession maps a session that could not start onto a Query outcome; nothing was sent.
func queryAttemptForSession[OUT any](failure *sessionFailure, providerRejected sdkgo.BranchID, defect sdkgo.BranchID) sdkgo.QueryAttempt[OUT] {
	var zero OUT
	switch failure.route {
	case sessionRetry:
		return sdkgo.NewQueryRetry[OUT](failure.failure, 0)
	case sessionRejected:
		return sdkgo.NewQueryBranch(providerRejected, zero, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewQueryBranch(defect, zero, &failure.failure, sdkgo.Receipt{})
	}
}

// exchange sends one request; after a 401 it forces one coordinated refresh and resends once,
// because Zoho Desk rejects an expired or revoked token before acting on the request.
func (client *Client) exchange(session *operationSession, operation string, request deskRequest) deskExchange {
	var payload []byte
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return deskExchange{outcome: exchangeDefect, failure: deskFailure(sdkgo.FailureLocalDefect, operation, errRequestNotBuilt.Error())}
		}
		payload = encoded
	}
	target := session.apiBaseURL + request.path
	if len(request.query) != 0 {
		target += "?" + request.query.Encode()
	}
	for attempt := 0; ; attempt++ {
		result := client.exchangeOnce(session, operation, request.method, target, payload)
		if result.response.statusCode != http.StatusUnauthorized || attempt > 0 {
			return result
		}
		if _, supportsRejection := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !supportsRejection {
			return result
		}
		replacement, err := sdkgo.ResolveCredentialAfterRejection(session.context, client.credentials, session.call, client.refreshDriver)
		if err != nil || validateResolvedCredentials(replacement) != nil || replacement.AuthMethodID != session.credentials.AuthMethodID {
			return result
		}
		session.credentials = replacement
	}
}

func (client *Client) exchangeOnce(session *operationSession, operation string, method string, target string, payload []byte) deskExchange {
	if session.context.Err() != nil {
		return deskExchange{outcome: exchangeNotSent, failure: deskFailure(sdkgo.FailureTransport, operation, "the operation deadline passed before Zoho Desk was contacted; no request was sent")}
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	observation := &dispatchObservation{}
	httpRequest, err := http.NewRequestWithContext(httptrace.WithClientTrace(session.context, observation.clientTrace()), method, target, body)
	if err != nil {
		return deskExchange{outcome: exchangeDefect, failure: deskFailure(sdkgo.FailureLocalDefect, operation, errRequestNotBuilt.Error())}
	}
	accessToken := session.credentials.AccessToken.Reveal()
	httpRequest.Header.Set("Authorization", authorizationScheme+accessToken)
	// Zoho Desk documents the header as orgId; Header.Set would send the canonical Orgid instead.
	httpRequest.Header[orgIDHeader] = []string{client.organizationID}
	httpRequest.Header.Set("Accept", "application/json")
	if payload != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if observation.isProvablyUndispatched() || isConnectionNeverEstablished(err) {
			return deskExchange{outcome: exchangeNotSent, failure: deskFailure(sdkgo.FailureTransport, operation, "Zoho Desk could not be reached; no request was sent")}
		}
		return deskExchange{outcome: exchangeUnavailable, failure: deskFailure(sdkgo.FailureTransport, operation, "Zoho Desk request failed before a response arrived")}
	}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	limit := client.maxResponseBytes
	if !isSuccess {
		limit = providerhttp.MaxErrorBodyBytes
	}
	content, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, limit)
	closeErr := httpResponse.Body.Close()
	response := deskResponse{
		statusCode: httpResponse.StatusCode, header: httpResponse.Header,
		requestCredits:   safeCreditCount(httpResponse.Header.Get(requestCreditsHeader)),
		remainingCredits: safeCreditCount(httpResponse.Header.Get(remainingCreditsHeader)),
	}
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge) && isSuccess:
		return deskExchange{outcome: exchangeInvalid, response: response, failure: deskFailure(sdkgo.FailureResponseTooLarge, operation, "Zoho Desk response exceeds the configured maxResponseBytes limit")}
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge):
		// An oversized error body carries no usable errorCode, so the status alone classifies it.
		content = nil
	case readErr != nil || closeErr != nil:
		return deskExchange{outcome: exchangeUnavailable, response: response, failure: deskFailure(sdkgo.FailureTransport, operation, "Zoho Desk response could not be read")}
	}
	if accessToken != "" && bytes.Contains(content, []byte(accessToken)) {
		if isSuccess {
			return deskExchange{outcome: exchangeInvalid, response: response, failure: deskFailure(sdkgo.FailureProtocol, operation, "Zoho Desk response reflected the connection credential")}
		}
		content = nil
	}
	response.body = content
	if isSuccess {
		return deskExchange{outcome: exchangeSucceeded, response: response}
	}
	return client.classifyFailureStatus(response, operation)
}

// classifyFailureStatus maps a non-2xx response without reading Zoho Desk's message text.
func (client *Client) classifyFailureStatus(response deskResponse, operation string) deskExchange {
	summary := describeDeskError(response.body)
	response.body = nil
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
	switch status := response.statusCode; {
	case status == http.StatusTooManyRequests:
		return deskExchange{outcome: exchangeRateLimited, response: response, retryAfter: retryAfter,
			failure: deskFailure(sdkgo.FailureRateLimit, operation, withErrorSummary("Zoho Desk rate limited the request (HTTP 429)", summary))}
	case status == http.StatusRequestTimeout || status >= 500:
		return deskExchange{outcome: exchangeUnavailable, response: response, retryAfter: retryAfter,
			failure: deskFailure(sdkgo.FailureAvailability, operation, withErrorSummary(fmt.Sprintf("Zoho Desk could not complete the request (HTTP %d)", status), summary))}
	case status == http.StatusNotFound:
		return deskExchange{outcome: exchangeNotFound, response: response,
			failure: deskFailure(sdkgo.FailureNotFound, operation, withErrorSummary("Zoho Desk found no such resource (HTTP 404)", summary))}
	case status >= 300 && status < 400:
		return deskExchange{outcome: exchangeRejected, response: response,
			failure: deskFailure(sdkgo.FailureProtocol, operation, fmt.Sprintf("Zoho Desk redirected the request (HTTP %d); check that the connection uses the account's data center", status))}
	case status == http.StatusUnauthorized:
		return deskExchange{outcome: exchangeRejected, response: response,
			failure: deskFailure(sdkgo.FailureAuthentication, operation, withErrorSummary("Zoho Desk rejected the access token (HTTP 401); reconnect the connection", summary))}
	case status == http.StatusForbidden:
		return deskExchange{outcome: exchangeRejected, response: response,
			failure: deskFailure(sdkgo.FailureAuthorization, operation, withErrorSummary(forbiddenMessage(summary.errorCode), summary))}
	default:
		return deskExchange{outcome: exchangeRejected, response: response,
			failure: deskFailure(rejectionFailureKind(status), operation, withErrorSummary(fmt.Sprintf("Zoho Desk rejected the request (HTTP %d)", status), summary))}
	}
}

func forbiddenMessage(errorCode string) string {
	switch errorCode {
	case errorCodeOrganizationMismatch:
		return "Zoho Desk refused the request (HTTP 403): the token is bound to another organization; set orgId to the organization chosen at Connect, or reconnect"
	case errorCodeScopeMismatch:
		return "Zoho Desk refused the request (HTTP 403): the token lacks a required scope; reconnect and accept every requested scope"
	default:
		return "Zoho Desk refused the request (HTTP 403)"
	}
}

func rejectionFailureKind(status int) sdkgo.FailureKind {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return sdkgo.FailureValidation
	case http.StatusConflict:
		return sdkgo.FailureConflict
	default:
		return sdkgo.FailureProviderRejection
	}
}

// describeDeskError reads only errorCode and errors[].fieldName/errorType; message text is never read.
func describeDeskError(body []byte) deskErrorSummary {
	var document struct {
		ErrorCode json.RawMessage `json:"errorCode"`
		Errors    []struct {
			FieldName json.RawMessage `json:"fieldName"`
			ErrorType json.RawMessage `json:"errorType"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &document) != nil {
		return deskErrorSummary{}
	}
	summary := deskErrorSummary{}
	if errorCode := jsonStringMatching(document.ErrorCode, errorCodePattern); errorCode != "" {
		summary.errorCode = errorCode
	}
	var fieldErrors []string
	for _, fieldError := range document.Errors {
		fieldName := jsonStringMatching(fieldError.FieldName, errorFieldNamePattern)
		if fieldName == "" {
			continue
		}
		if errorType := jsonStringMatching(fieldError.ErrorType, errorTypePattern); errorType != "" {
			fieldName += "=" + errorType
		}
		fieldErrors = append(fieldErrors, fieldName)
	}
	sort.Strings(fieldErrors)
	summary.fieldErrors = fieldErrors[:min(len(fieldErrors), maximumReportedErrorDetails)]
	return summary
}

func jsonStringMatching(raw json.RawMessage, pattern *regexp.Regexp) string {
	var value string
	if json.Unmarshal(raw, &value) != nil || !pattern.MatchString(value) {
		return ""
	}
	return value
}

func withErrorSummary(message string, summary deskErrorSummary) string {
	var parts []string
	if summary.errorCode != "" {
		parts = append(parts, summary.errorCode)
	}
	if len(summary.fieldErrors) != 0 {
		parts = append(parts, "errors: "+strings.Join(summary.fieldErrors, ", "))
	}
	if len(parts) == 0 {
		return message
	}
	return message + " [" + strings.Join(parts, "; ") + "]"
}

// receipt records the call identity, the Zoho object, and Zoho Desk's credit headers when present.
func (client *Client) receipt(call sdkgo.Call, response deskResponse, objectID string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
	if response.requestCredits != "" || response.remainingCredits != "" {
		receipt.Metadata = map[string]string{}
		if response.requestCredits != "" {
			receipt.Metadata["requestCredits"] = response.requestCredits
		}
		if response.remainingCredits != "" {
			receipt.Metadata["remainingCredits"] = response.remainingCredits
		}
	}
	return receipt
}

// isConnectionNeverEstablished reports a dial failure, after which Zoho Desk cannot have received the request.
func isConnectionNeverEstablished(err error) bool {
	var operationError *net.OpError
	return errors.As(err, &operationError) && operationError.Op == "dial"
}

func (observation *dispatchObservation) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSDone:          observation.recordDNSDone,
		ConnectDone:      observation.recordConnectDone,
		TLSHandshakeDone: observation.recordTLSHandshakeDone,
		GotConn:          observation.recordObtainedConnection,
	}
}

// isProvablyUndispatched requires a traced DNS, connect, or TLS failure and no obtained connection,
// such as a connect that the deadline cut short.
func (observation *dispatchObservation) isProvablyUndispatched() bool {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	return observation.hasFailedToConnect && !observation.hasObtainedConnection
}

func (observation *dispatchObservation) recordDNSDone(info httptrace.DNSDoneInfo) {
	if info.Err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordConnectDone(_ string, _ string, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordTLSHandshakeDone(_ tls.ConnectionState, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordObtainedConnection(httptrace.GotConnInfo) {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasObtainedConnection = true
}

func (observation *dispatchObservation) recordConnectionFailure() {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasFailedToConnect = true
}

func safeCreditCount(value string) string {
	if creditCountPattern.MatchString(value) {
		return value
	}
	return ""
}

func ticketPath(ticketID string) string {
	return "/tickets/" + url.PathEscape(ticketID)
}

// validateResolvedCredentials accepts a credential that can authorize one request in a known data center.
func validateResolvedCredentials(credentials Credentials) error {
	if _, isKnown := DataCenterForAuthMethod(credentials.AuthMethodID); !isKnown {
		return errors.New("Zoho Desk authorization method is not a supported data center")
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Zoho access token is missing or is not a valid header value")
	}
	return nil
}

func validateZohoID(fieldName string, value string) error {
	if !zohoIDPattern.MatchString(value) {
		return fmt.Errorf("%s must be a numeric Zoho Desk ID such as 1892000000042034", fieldName)
	}
	return nil
}

func validateOptionalZohoID(fieldName string, value string) error {
	if value == "" {
		return nil
	}
	return validateZohoID(fieldName, value)
}

func deskFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func deskFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := deskFailure(kind, operation, message)
	return &failure
}
