// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package outlookcalendar implements bounded Microsoft Outlook calendar reads, idempotent
// event writes, and free/busy queries through Microsoft Graph v1.0 as Dex connector Steps.
//
// A connection authorizes either one work or school user through delegated Microsoft OAuth,
// whose calls address /me, or an app-only Entra application whose client credentials reach
// the one mailbox the connection configures, whose calls address /users/{mailbox}.
//
// Every time the package accepts is explicit: an RFC 3339 instant with a Z or ±hh:mm
// offset plus an IANA time zone. Every time it returns is requested from Graph in UTC and
// rendered with an offset in the caller's zone. See EventDateTime.
package outlookcalendar

import (
	"bytes"
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
	providerName = "outlook-calendar"
	graphHost    = "graph.microsoft.com"
	identityHost = "login.microsoftonline.com"
	graphBaseURL = "https://" + graphHost + "/v1.0"

	// defaultRequestTimeout bounds one Graph request inside the 30-second Execute timeout.
	defaultRequestTimeout = 20 * time.Second
	// preferUTCTimeZone asks Graph to return every dateTimeTimeZone in UTC, which has no ambiguous wall times.
	preferUTCTimeZone = `outlook.timezone="UTC"`
	// preferTextBody asks Graph to return event bodies as plain text instead of HTML.
	preferTextBody = `outlook.body-content-type="text"`
)

var (
	// graphErrorCodePattern keeps only a word-shaped error.code; Graph error messages are never read.
	graphErrorCodePattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,63}$`)
	graphRequestIDPattern  = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	mailboxObjectIDPattern = tenantGUIDPattern
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
// client carries token requests to https://login.microsoftonline.com.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithLocalProviderURL sends every request for https://graph.microsoft.com and
// https://login.microsoftonline.com to one local Graph-compatible fake, keeping each
// request's path, for local verification only. The URL must be an http or https URL whose
// host is loopback, without a path, user information, a query, or a fragment, and the
// transport refuses every other host. Production connections leave it unset.
func WithLocalProviderURL(baseURL string) Option {
	return func(options *clientOptions) { options.localProviderURL = baseURL }
}

// WithClock overrides the clock used for Receipts and token expiry. Tests use it;
// production connections leave it unset.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// Client executes authenticated Microsoft Graph calendar requests for connector operations.
// A Client is safe for concurrent use by several Steps.
type Client struct {
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	refreshDriver    *CredentialRefreshDriver
	mailbox          string
	maxResponseBytes int64
	now              func() time.Time
}

type graphRequest struct {
	method string
	// path is relative to the Graph v1.0 base URL and already includes the mailbox root.
	path string
	// nextLink replaces path and query with a validated @odata.nextLink URL.
	nextLink string
	query    url.Values
	payload  any
	ifMatch  string
}

type graphResponse struct {
	statusCode int
	header     http.Header
	body       []byte
	requestID  string
}

// providerRequestError is a safe local classification of a request that produced no usable response.
type providerRequestError struct {
	kind    sdkgo.FailureKind
	message string
}

// Error returns the safe human-readable failure message.
func (failure *providerRequestError) Error() string { return failure.message }

// statusClassification is the safe meaning of a non-success Graph response.
type statusClassification struct {
	isRetryable bool
	retryAfter  time.Duration
	kind        sdkgo.FailureKind
	message     string
}

type eventReadOutcome uint8

const (
	eventReadFound eventReadOutcome = iota + 1
	eventReadMissing
	eventReadRetry
	eventReadRejected
	eventReadInvalid
	eventReadDefect
)

// eventReadResult is one classified event read that each operation maps to its own branch.
type eventReadResult struct {
	outcome    eventReadOutcome
	event      Event
	response   graphResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
	// duplicateEventIDs lists further events a marker lookup found, which only a missed transactionId guard creates.
	duplicateEventIDs []string
}

// New validates configuration and constructs a Microsoft Graph calendar client. Credentials
// are resolved before every provider call, so a refreshed token takes effect without a
// restart; the tenant, mailbox, and response limit are startup configuration. Construction
// never contacts Microsoft.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Outlook Calendar response limit must be positive")
	}
	tenantID := strings.TrimSpace(config.TenantID)
	if tenantID != "" {
		if err := ValidateTenantID(tenantID); err != nil {
			return nil, err
		}
	}
	mailbox := strings.TrimSpace(config.Mailbox)
	if mailbox != "" {
		if err := ValidateMailbox(mailbox); err != nil {
			return nil, err
		}
	}
	if credentials == nil {
		return nil, errors.New("Outlook Calendar credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Outlook Calendar connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("Outlook Calendar connector clock is required")
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
	refreshDriver := NewCredentialRefreshDriver(httpClient, tenantID)
	refreshDriver.now = dependencies.now
	return &Client{
		httpClient: httpClient, credentials: credentials, refreshDriver: refreshDriver, mailbox: mailbox,
		maxResponseBytes: config.MaxResponseBytes, now: dependencies.now,
	}, nil
}

// ValidateMailbox reports whether mailbox names one Exchange Online mailbox for an app-only
// connection: a bare SMTP address or user principal name such as scheduling@contoso.com, or
// the user's Entra object ID GUID. The error never repeats the value.
func ValidateMailbox(mailbox string) error {
	if mailboxObjectIDPattern.MatchString(mailbox) {
		return nil
	}
	if validateBareEmailAddress("mailbox", mailbox) != nil {
		return errors.New("mailbox must be a bare address such as scheduling@contoso.com or a user object ID GUID")
	}
	return nil
}

// ListEvents returns the listEvents Query bound to this client.
func (client *Client) ListEvents() ListEventsOperation { return ListEventsOperation{client: client} }

// GetEvent returns the getEvent Query bound to this client.
func (client *Client) GetEvent() GetEventOperation { return GetEventOperation{client: client} }

// CreateEvent returns the createEvent Mutation bound to this client.
func (client *Client) CreateEvent() CreateEventOperation { return CreateEventOperation{client: client} }

// UpdateEvent returns the updateEvent Mutation bound to this client.
func (client *Client) UpdateEvent() UpdateEventOperation { return UpdateEventOperation{client: client} }

// QueryFreeBusy returns the queryFreeBusy Query bound to this client.
func (client *Client) QueryFreeBusy() QueryFreeBusyOperation {
	return QueryFreeBusyOperation{client: client}
}

// resolveCredentials returns the call's credentials and the Graph path of the mailbox they reach.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, string, *sdkgo.Failure) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return Credentials{}, "", failurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable")
	}
	if credentials.AuthMethodID == MicrosoftOAuthAuthMethodID {
		return credentials, "/me", nil
	}
	if client.mailbox == "" {
		return Credentials{}, "", failurePointer(sdkgo.FailureValidation, operation, "an app-only connection needs its mailbox configured")
	}
	return credentials, "/users/" + url.PathEscape(client.mailbox), nil
}

// readEvent performs one event GET and classifies it without choosing an operation branch.
func (client *Client) readEvent(call sdkgo.Call, credentials *Credentials, operation string, mailboxRoot string, eventID string, display eventDisplay) eventReadResult {
	response, err := client.send(call, credentials, graphRequest{
		method: http.MethodGet, path: mailboxRoot + "/events/" + url.PathEscape(eventID),
		query: url.Values{"$select": {eventSelectFields}},
	})
	if err != nil {
		return classifyReadTransportError(err, response, operation)
	}
	if isMissingResourceStatus(response.statusCode) {
		return eventReadResult{outcome: eventReadMissing, response: response, failure: calendarFailure(sdkgo.FailureNotFound, operation, "event was not found")}
	}
	if !isSuccessStatus(response.statusCode) {
		return classifyReadStatus(response, operation)
	}
	event, decodeErr := decodeEvent(response.body, "", display)
	if decodeErr != nil {
		return eventReadResult{outcome: eventReadInvalid, response: response, failure: calendarFailure(sdkgo.FailureProtocol, operation, "provider returned an invalid event: "+decodeErr.Error())}
	}
	return eventReadResult{outcome: eventReadFound, response: response, event: event}
}

// findEventByIdempotencyKey finds the event stamped with key through Graph's mailbox-wide extended-property $filter.
func (client *Client) findEventByIdempotencyKey(call sdkgo.Call, credentials *Credentials, operation string, mailboxRoot string, key string, calendarID string, display eventDisplay) eventReadResult {
	filter := fmt.Sprintf("singleValueExtendedProperties/Any(ep: ep/id eq '%s' and ep/value eq '%s')", idempotencyMarkerPropertyID, key)
	response, err := client.send(call, credentials, graphRequest{
		method: http.MethodGet, path: mailboxRoot + "/events",
		query: url.Values{"$filter": {filter}, "$select": {eventSelectFields}, "$top": {"10"}},
	})
	if err != nil {
		return classifyReadTransportError(err, response, operation)
	}
	if !isSuccessStatus(response.statusCode) {
		return classifyReadStatus(response, operation)
	}
	var page struct {
		Value []graphEvent `json:"value"`
	}
	if json.Unmarshal(response.body, &page) != nil || page.Value == nil {
		return eventReadResult{outcome: eventReadInvalid, response: response, failure: calendarFailure(sdkgo.FailureProtocol, operation, "provider returned an invalid event lookup")}
	}
	if len(page.Value) == 0 {
		return eventReadResult{outcome: eventReadMissing, response: response, failure: calendarFailure(sdkgo.FailureNotFound, operation, "no event carries the idempotency key")}
	}
	result := eventReadResult{outcome: eventReadFound, response: response}
	for index, resource := range page.Value {
		event, decodeErr := convertEvent(resource, calendarID, display)
		if decodeErr != nil {
			return eventReadResult{outcome: eventReadInvalid, response: response, failure: calendarFailure(sdkgo.FailureProtocol, operation, "provider returned an invalid event: "+decodeErr.Error())}
		}
		if index == 0 {
			result.event = event
			continue
		}
		result.duplicateEventIDs = append(result.duplicateEventIDs, event.ID)
	}
	return result
}

func classifyReadTransportError(err error, response graphResponse, operation string) eventReadResult {
	requestFailure := asProviderRequestError(err)
	switch requestFailure.kind {
	case sdkgo.FailureResponseTooLarge:
		return eventReadResult{outcome: eventReadInvalid, response: response, failure: calendarFailure(requestFailure.kind, operation, requestFailure.message)}
	case sdkgo.FailureLocalDefect:
		return eventReadResult{outcome: eventReadDefect, failure: calendarFailure(requestFailure.kind, operation, requestFailure.message)}
	default:
		return eventReadResult{outcome: eventReadRetry, failure: calendarFailure(sdkgo.FailureAvailability, operation, "provider is unavailable")}
	}
}

func classifyReadStatus(response graphResponse, operation string) eventReadResult {
	classification := classifyFailureStatus(response)
	outcome := eventReadRejected
	if classification.isRetryable {
		outcome = eventReadRetry
	}
	return eventReadResult{outcome: outcome, response: response, retryAfter: classification.retryAfter, failure: calendarFailure(classification.kind, operation, classification.message)}
}

// send issues one authenticated request. After a 401 it refreshes once, updates credentials, and retries once.
func (client *Client) send(call sdkgo.Call, credentials *Credentials, request graphRequest) (graphResponse, error) {
	var encodedPayload []byte
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return graphResponse{}, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be encoded"}
		}
		encodedPayload = encoded
	}
	target := graphBaseURL + request.path + "?" + encodeGraphQuery(request.query)
	if request.nextLink != "" {
		target = request.nextLink
	}
	for attempt := 0; attempt < 2; attempt++ {
		var body io.Reader
		if encodedPayload != nil {
			body = bytes.NewReader(encodedPayload)
		}
		httpRequest, err := http.NewRequestWithContext(call.Context, request.method, strings.TrimSuffix(target, "?"), body)
		if err != nil {
			return graphResponse{}, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be built"}
		}
		httpRequest.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
		httpRequest.Header.Set("Accept", "application/json")
		httpRequest.Header.Add("Prefer", preferUTCTimeZone)
		httpRequest.Header.Add("Prefer", preferTextBody)
		// Graph echoes client-request-id, so a support case can name the Dex call.
		httpRequest.Header.Set("client-request-id", string(call.ID))
		if encodedPayload != nil {
			httpRequest.Header.Set("Content-Type", "application/json")
		}
		if request.ifMatch != "" {
			httpRequest.Header.Set("If-Match", request.ifMatch)
		}
		response, err := client.httpClient.Do(httpRequest)
		if err != nil {
			return graphResponse{}, &providerRequestError{kind: sdkgo.FailureTransport, message: "provider request failed"}
		}
		content, readErr := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
		closeErr := response.Body.Close()
		result := graphResponse{statusCode: response.StatusCode, header: response.Header, requestID: graphRequestID(response.Header)}
		if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
			return result, &providerRequestError{kind: sdkgo.FailureResponseTooLarge, message: "provider response exceeds configured limit"}
		}
		if readErr != nil || closeErr != nil {
			return graphResponse{}, &providerRequestError{kind: sdkgo.FailureTransport, message: "provider response could not be read"}
		}
		result.body = content
		if response.StatusCode != http.StatusUnauthorized || attempt != 0 {
			return result, nil
		}
		if _, ok := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !ok {
			return result, nil
		}
		replacement, refreshErr := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		if refreshErr != nil || validateResolvedCredentials(replacement) != nil {
			return result, nil
		}
		*credentials = replacement
	}
	return graphResponse{}, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "authenticated request retry was exhausted"}
}

// encodeGraphQuery percent-encodes spaces as %20, because Graph's OData parser does not read + as a space.
func encodeGraphQuery(query url.Values) string {
	return strings.ReplaceAll(query.Encode(), "+", "%20")
}

// validateNextLink accepts only an https://graph.microsoft.com/v1.0 calendarView page URL.
func validateNextLink(nextLink string) error {
	parsed, err := url.Parse(nextLink)
	if err != nil || parsed.Scheme != "https" || parsed.Host != graphHost || parsed.User != nil || parsed.Fragment != "" {
		return errors.New("pageToken must be the nextPageToken of a previous page, an https://graph.microsoft.com URL")
	}
	if !strings.HasPrefix(parsed.Path, "/v1.0/") || !strings.HasSuffix(strings.ToLower(parsed.Path), "/calendarview") {
		return errors.New("pageToken must continue a Microsoft Graph v1.0 calendarView listing")
	}
	return nil
}

// classifyFailureStatus maps a non-2xx response from its status and error.code, never its message.
func classifyFailureStatus(response graphResponse) statusClassification {
	code := graphErrorCode(response.body)
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), time.Now())
	switch response.statusCode {
	case http.StatusTooManyRequests:
		return statusClassification{isRetryable: true, retryAfter: retryAfter, kind: sdkgo.FailureRateLimit, message: describeStatus("provider throttled the request", code)}
	case http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return statusClassification{isRetryable: true, retryAfter: retryAfter, kind: sdkgo.FailureAvailability, message: describeStatus("provider is temporarily unavailable", code)}
	}
	kind := statusFailureKind(response.statusCode)
	if code == "quotaLimitReached" || code == "ErrorQuotaExceeded" {
		kind = sdkgo.FailureQuotaExhausted
	}
	return statusClassification{kind: kind, message: describeStatus(fmt.Sprintf("provider rejected the request with HTTP %d", response.statusCode), code)}
}

// graphErrorCode returns Graph's error.code when it is word-shaped; error.message is never read.
func graphErrorCode(body []byte) string {
	for _, token := range providerhttp.ReadErrorTokens(body, []string{"/error/code"}) {
		if graphErrorCodePattern.MatchString(token) {
			return token
		}
	}
	return ""
}

func describeStatus(message string, code string) string {
	if code == "" {
		return message
	}
	return message + " (" + code + ")"
}

func isSuccessStatus(status int) bool { return status >= 200 && status < 300 }

func isMissingResourceStatus(status int) bool {
	return status == http.StatusNotFound || status == http.StatusGone
}

func asProviderRequestError(err error) *providerRequestError {
	var requestFailure *providerRequestError
	if errors.As(err, &requestFailure) {
		return requestFailure
	}
	return &providerRequestError{kind: sdkgo.FailureTransport, message: "provider request failed"}
}

func statusFailureKind(status int) sdkgo.FailureKind {
	switch status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return sdkgo.FailureValidation
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case http.StatusNotFound, http.StatusGone:
		return sdkgo.FailureNotFound
	case http.StatusConflict, http.StatusPreconditionFailed:
		return sdkgo.FailureConflict
	default:
		return sdkgo.FailureProviderRejection
	}
}

// calendarPath returns the events collection of calendarID, or of the mailbox's default calendar when blank.
func calendarPath(mailboxRoot string, calendarID string) string {
	if calendarID == "" {
		return mailboxRoot + "/calendar"
	}
	return mailboxRoot + "/calendars/" + url.PathEscape(calendarID)
}

func calendarFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func failurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := calendarFailure(kind, operation, message)
	return &failure
}

func graphRequestID(header http.Header) string {
	if value := header.Get("request-id"); graphRequestIDPattern.MatchString(value) {
		return value
	}
	return ""
}

func (client *Client) receipt(call sdkgo.Call, response graphResponse, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ProviderRequestID: response.requestID, ObservedAt: client.now().UTC(),
	}
}
