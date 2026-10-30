// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package calendar implements bounded Google Calendar event, free/busy, and
// idempotent event-write operations as Dex connector Steps.
//
// Every time the package accepts is explicit: a timed boundary is an RFC 3339
// instant with a Z or ±hh:mm offset plus an IANA time zone, and an all-day
// boundary is a date without a time zone. See EventDateTime.
package calendar

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const providerName = "google-calendar"

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient overrides the default 25-second HTTP client; the caller retains ownership.
// The client is shared by operation requests and credential refresh.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Google Calendar requests for connector operations.
// A Client is safe for concurrent use by several Steps.
type Client struct {
	endpoint         *url.URL
	httpClient       *http.Client
	credentials      CredentialSource
	refreshDriver    sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes int64
	now              func() time.Time
}

type providerRequest struct {
	method     string
	pathSuffix string
	query      url.Values
	payload    any
	ifMatch    string
}

type providerResponse struct {
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

// statusClassification is the safe, provider-neutral meaning of a non-success Google response.
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

// eventReadResult is one classified events.get response that each operation maps to its own branch.
type eventReadResult struct {
	outcome    eventReadOutcome
	event      Event
	response   providerResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

type googleErrorEnvelope struct {
	Error struct {
		Errors []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
	} `json:"error"`
}

// reportableGoogleErrorReasons are documented Calendar reason codes safe to repeat in a Failure.
var reportableGoogleErrorReasons = map[string]bool{
	"badRequest": true, "invalid": true, "required": true, "timeRangeEmpty": true,
	"notFound": true, "deleted": true, "duplicate": true, "conditionNotMet": true,
	"forbidden": true, "forbiddenForNonOrganizer": true, "requiredAccessLevel": true,
	"insufficientPermissions": true, "quotaExceeded": true, "dailyLimitExceeded": true,
	"rateLimitExceeded": true, "userRateLimitExceeded": true, "backendError": true,
	"invalidAttendeeEmail": true, "cannotChangeOrganizer": true, "eventTypeRestriction": true,
	"authError": true,
}

// New validates configuration and constructs an authenticated Google Calendar client.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Hostname() == "" {
		return nil, fmt.Errorf("Google Calendar endpoint must be absolute")
	}
	if endpoint.Scheme != "https" && endpoint.Hostname() != "localhost" && endpoint.Hostname() != "127.0.0.1" {
		return nil, fmt.Errorf("Google Calendar endpoint must use HTTPS")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Google Calendar connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.httpClient == nil {
		dependencies.httpClient = &http.Client{Timeout: 25 * time.Second}
	}
	if config.MaxResponseBytes < 1 {
		return nil, fmt.Errorf("Google Calendar response limit must be positive")
	}
	return &Client{
		endpoint: endpoint, httpClient: dependencies.httpClient, credentials: credentials,
		refreshDriver:    NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes: config.MaxResponseBytes, now: time.Now,
	}, nil
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

func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return Credentials{}, failurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable")
	}
	return credentials, nil
}

// readEvent performs one events.get request and classifies it without choosing an operation branch.
func (client *Client) readEvent(call sdkgo.Call, credentials *Credentials, operation string, calendarID string, eventID string, timeZone string) eventReadResult {
	query := url.Values{}
	if timeZone != "" {
		query.Set("timeZone", timeZone)
	}
	response, err := client.send(call, credentials, providerRequest{method: http.MethodGet, pathSuffix: eventPath(calendarID, eventID), query: query})
	if err != nil {
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
	if isMissingResourceStatus(response.statusCode) {
		return eventReadResult{outcome: eventReadMissing, response: response, failure: calendarFailure(sdkgo.FailureNotFound, operation, "calendar or event was not found")}
	}
	if !isSuccessStatus(response.statusCode) {
		classification := classifyFailureStatus(response)
		outcome := eventReadRejected
		if classification.isRetryable {
			outcome = eventReadRetry
		}
		return eventReadResult{outcome: outcome, response: response, retryAfter: classification.retryAfter, failure: calendarFailure(classification.kind, operation, classification.message)}
	}
	event, decodeErr := decodeEvent(response.body, calendarID)
	if decodeErr != nil {
		return eventReadResult{outcome: eventReadInvalid, response: response, failure: calendarFailure(sdkgo.FailureProtocol, operation, "provider returned an invalid event: "+decodeErr.Error())}
	}
	return eventReadResult{outcome: eventReadFound, response: response, event: event}
}

// send issues one authenticated request. After a 401 it refreshes once, updates credentials, and retries once.
func (client *Client) send(call sdkgo.Call, credentials *Credentials, request providerRequest) (providerResponse, error) {
	var encodedPayload []byte
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return providerResponse{}, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be encoded"}
		}
		encodedPayload = encoded
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + request.pathSuffix
	for attempt := 0; attempt < 2; attempt++ {
		var body io.Reader
		if encodedPayload != nil {
			body = bytes.NewReader(encodedPayload)
		}
		httpRequest, err := http.NewRequestWithContext(call.Context, request.method, target, body)
		if err != nil {
			return providerResponse{}, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be built"}
		}
		httpRequest.URL.RawQuery = request.query.Encode()
		httpRequest.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
		httpRequest.Header.Set("Accept", "application/json")
		if encodedPayload != nil {
			httpRequest.Header.Set("Content-Type", "application/json")
		}
		if request.ifMatch != "" {
			httpRequest.Header.Set("If-Match", request.ifMatch)
		}
		response, err := client.httpClient.Do(httpRequest)
		if err != nil {
			return providerResponse{}, &providerRequestError{kind: sdkgo.FailureTransport, message: "provider request failed"}
		}
		content, readErr := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			return providerResponse{}, &providerRequestError{kind: sdkgo.FailureTransport, message: "provider response could not be read"}
		}
		result := providerResponse{statusCode: response.StatusCode, header: response.Header, body: content, requestID: googleRequestID(response.Header)}
		if int64(len(content)) > client.maxResponseBytes {
			result.body = nil
			return result, &providerRequestError{kind: sdkgo.FailureResponseTooLarge, message: "provider response exceeds configured limit"}
		}
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
	return providerResponse{}, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "authenticated request retry was exhausted"}
}

// classifyFailureStatus maps a non-2xx response; Google also reports rate limits as 403 with a rate-limit reason.
func classifyFailureStatus(response providerResponse) statusClassification {
	reason := googleErrorReason(response.body)
	switch {
	case response.statusCode == http.StatusTooManyRequests,
		response.statusCode == http.StatusForbidden && (reason == "rateLimitExceeded" || reason == "userRateLimitExceeded"):
		delay, err := retryAfterDelay(response.header)
		if err != nil {
			return statusClassification{isRetryable: true, kind: sdkgo.FailureProtocol, message: "provider returned an invalid Retry-After header"}
		}
		return statusClassification{isRetryable: true, retryAfter: delay, kind: sdkgo.FailureRateLimit, message: describeStatus("provider rate limited the request", reason)}
	case response.statusCode == http.StatusRequestTimeout || response.statusCode >= 500:
		delay, err := retryAfterDelay(response.header)
		if err != nil {
			return statusClassification{isRetryable: true, kind: sdkgo.FailureProtocol, message: "provider returned an invalid Retry-After header"}
		}
		return statusClassification{isRetryable: true, retryAfter: delay, kind: sdkgo.FailureAvailability, message: describeStatus("provider is temporarily unavailable", reason)}
	case reason == "quotaExceeded" || reason == "dailyLimitExceeded":
		return statusClassification{kind: sdkgo.FailureQuotaExhausted, message: describeStatus("provider quota is exhausted", reason)}
	default:
		return statusClassification{kind: statusFailureKind(response.statusCode), message: describeStatus(fmt.Sprintf("provider rejected the request with HTTP %d", response.statusCode), reason)}
	}
}

func googleErrorReason(body []byte) string {
	var envelope googleErrorEnvelope
	if len(body) == 0 || json.Unmarshal(body, &envelope) != nil {
		return ""
	}
	for _, detail := range envelope.Error.Errors {
		if reportableGoogleErrorReasons[detail.Reason] {
			return detail.Reason
		}
	}
	return ""
}

func describeStatus(message string, reason string) string {
	if reason == "" {
		return message
	}
	return message + " (" + reason + ")"
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

func retryAfterDelay(header http.Header) (time.Duration, error) {
	value := header.Get("Retry-After")
	if value == "" {
		return 0, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse Retry-After %q: %w", value, err)
	}
	if seconds < 0 {
		return 0, fmt.Errorf("parse Retry-After %q: value cannot be negative", value)
	}
	return time.Duration(seconds) * time.Second, nil
}

func statusFailureKind(status int) sdkgo.FailureKind {
	switch status {
	case http.StatusBadRequest:
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

func calendarPath(calendarID string) string {
	return "/calendars/" + url.PathEscape(calendarID)
}

func eventPath(calendarID string, eventID string) string {
	return calendarPath(calendarID) + "/events/" + url.PathEscape(eventID)
}

func calendarFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func failurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := calendarFailure(kind, operation, message)
	return &failure
}

func googleRequestID(header http.Header) string {
	if value := header.Get("X-Goog-Request-Id"); value != "" {
		return value
	}
	return header.Get("X-Request-Id")
}

func (client *Client) receipt(call sdkgo.Call, response providerResponse, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ProviderRequestID: response.requestID, ObservedAt: client.now().UTC(),
	}
}
