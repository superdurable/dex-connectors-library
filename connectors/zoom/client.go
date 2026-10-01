// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package zoom implements the authorized user's Zoom Meetings operations as Dex
// connector Steps: list, read, schedule, and reschedule meetings, and list who
// joined a past meeting.
//
// Every time the package accepts is explicit: a start is an RFC 3339 instant
// with a Z or ±hh:mm offset plus an IANA time zone, and the connector sends the
// instant to Zoom in UTC. Zoom has no idempotency key, so createMeeting reports
// an unknown outcome on its uncertain branch instead of creating again.
package zoom

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "zoom"
	// requestTimeout expires before the 30-second Execute timeout, so a hung create selects uncertain.
	requestTimeout = 20 * time.Second
	// maximumThrottleDelay separates a per-second throttle from a daily limit that resets much later.
	maximumThrottleDelay = time.Minute
	// errorCodeMetadataKey names the Receipt metadata entry holding Zoom's numeric error code.
	errorCodeMetadataKey = "zoomErrorCode"
	currentUserPath      = "/users/me"
)

var (
	errRequestNotBuilt = errors.New("Zoom request could not be built")
	// zoomErrorCodePointers locate Zoom's numeric error code; message text is never read.
	zoomErrorCodePointers = []string{"/code"}
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient replaces the HTTP client used for Zoom API calls and token
// refresh. The caller keeps ownership of client and its transport. The
// connector uses a copy that never follows redirects, and it bounds every API
// request by 20 seconds even when client allows longer, so a hung create
// selects uncertain before Dex's 30-second Execute timeout.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Zoom API calls for the authorized user. It is
// safe for concurrent use by several Steps.
type Client struct {
	baseURL          string
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	refreshDriver    *CredentialRefreshDriver
	maxResponseBytes int64
	now              func() time.Time
}

type providerRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

type providerResponse struct {
	statusCode            int
	header                http.Header
	body                  []byte
	isBodyTooLarge        bool
	hasBodyReadFailed     bool
	isCredentialReflected bool
}

// statusOutcome is how an operation may treat a completed Zoom response.
type statusOutcome uint8

const (
	statusOutcomeUsable statusOutcome = iota + 1
	statusOutcomeRetry
	statusOutcomeNotFound
	statusOutcomeRejected
	// statusOutcomeInvalid is a redirect or an oversized or credential-reflecting body.
	statusOutcomeInvalid
)

// statusClassification is the safe meaning of one completed Zoom response.
type statusClassification struct {
	outcome    statusOutcome
	retryAfter time.Duration
	failure    sdkgo.Failure
	errorCode  string
}

// dispatchObservation records whether a request could have reached Zoom.
type dispatchObservation struct {
	mutex                 sync.Mutex
	hasObtainedConnection bool
	hasFailedToConnect    bool
}

// New validates configuration and constructs a Zoom client. A blank endpoint
// uses Zoom's global API, and credentials are resolved before every call.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Zoom endpoint: %w", err)
	}
	if config.MaxResponseBytes < 1 {
		return nil, fmt.Errorf("Zoom maxResponseBytes must be positive")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Zoom connector option is nil")
		}
		option(&dependencies)
	}
	httpClient := providerhttp.NewProviderHTTPClient(dependencies.httpClient, requestTimeout)
	return &Client{
		baseURL: baseURL, httpClient: httpClient, credentials: credentials,
		refreshDriver:    NewCredentialRefreshDriver(httpClient),
		maxResponseBytes: config.MaxResponseBytes, now: time.Now,
	}, nil
}

// ListMeetings returns the listMeetings Query bound to this client.
func (client *Client) ListMeetings() ListMeetingsOperation {
	return ListMeetingsOperation{client: client}
}

// GetMeeting returns the getMeeting Query bound to this client.
func (client *Client) GetMeeting() GetMeetingOperation { return GetMeetingOperation{client: client} }

// CreateMeeting returns the createMeeting Mutation bound to this client.
func (client *Client) CreateMeeting() CreateMeetingOperation {
	return CreateMeetingOperation{client: client}
}

// UpdateMeeting returns the updateMeeting Mutation bound to this client.
func (client *Client) UpdateMeeting() UpdateMeetingOperation {
	return UpdateMeetingOperation{client: client}
}

// ListPastMeetingParticipants returns the listPastMeetingParticipants Query bound to this client.
func (client *Client) ListPastMeetingParticipants() ListPastMeetingParticipantsOperation {
	return ListPastMeetingParticipantsOperation{client: client}
}

// resolveCredentials also reports whether a retry can help, as after a token endpoint outage.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure, bool) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return Credentials{}, failurePointer(operation, sdkgo.FailureAuthentication, "Zoom connection requires reauthorization"), false
	case err != nil:
		return Credentials{}, failurePointer(operation, sdkgo.FailureAvailability, "Zoom connection credentials could not be refreshed"), true
	case validateResolvedCredentials(credentials) != nil:
		return Credentials{}, failurePointer(operation, sdkgo.FailureAuthentication, "Zoom connection credentials are unavailable"), false
	default:
		return credentials, nil, false
	}
}

// exchange sends one request, and once more after a refresh when Zoom answers 401, which applies nothing.
// It returns isDispatched false only when Zoom provably received nothing.
func (client *Client) exchange(
	call sdkgo.Call,
	credentials *Credentials,
	request providerRequest,
) (response providerResponse, isDispatched bool, err error) {
	var encodedPayload []byte
	if request.payload != nil {
		encodedPayload, err = json.Marshal(request.payload)
		if err != nil {
			return providerResponse{}, false, errRequestNotBuilt
		}
	}
	target := client.baseURL + request.path
	if len(request.query) > 0 {
		target += "?" + request.query.Encode()
	}
	for attempt := 0; attempt < 2; attempt++ {
		response, isDispatched, err = client.sendOnce(call, credentials.AccessToken, request.method, target, encodedPayload)
		if err != nil || response.statusCode != http.StatusUnauthorized || attempt != 0 {
			return response, isDispatched, err
		}
		if _, isRejectionRefreshing := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !isRejectionRefreshing {
			return response, isDispatched, nil
		}
		replacement, refreshErr := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		if refreshErr != nil || validateResolvedCredentials(replacement) != nil {
			return response, isDispatched, nil
		}
		*credentials = replacement
	}
	return response, isDispatched, nil
}

// sendOnce performs one bounded HTTP request and reads a bounded body.
func (client *Client) sendOnce(
	call sdkgo.Call,
	accessToken sdkgo.SecretString,
	method string,
	target string,
	payload []byte,
) (providerResponse, bool, error) {
	requestContext, cancel := context.WithTimeout(call.Context, requestTimeout)
	defer cancel()
	observation := &dispatchObservation{}
	requestContext = httptrace.WithClientTrace(requestContext, observation.clientTrace())
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	httpRequest, err := http.NewRequestWithContext(requestContext, method, target, body)
	if err != nil {
		return providerResponse{}, false, errRequestNotBuilt
	}
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken.Reveal())
	httpRequest.Header.Set("Accept", "application/json")
	if payload != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return providerResponse{}, !observation.isProvablyUndispatched(), err
	}
	defer func() {
		// The body is fully read or bounded below; a close failure cannot change the classified response.
		_ = httpResponse.Body.Close()
	}()
	response := providerResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
	if !isSuccessStatus(httpResponse.StatusCode) {
		response.body, err = io.ReadAll(io.LimitReader(httpResponse.Body, providerhttp.MaxErrorBodyBytes))
		response.hasBodyReadFailed = err != nil
		return response, true, nil
	}
	response.body, err = providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	response.isBodyTooLarge = errors.Is(err, providerhttp.ErrBodyTooLarge)
	response.hasBodyReadFailed = err != nil && !response.isBodyTooLarge
	if accessToken.Reveal() != "" && bytes.Contains(response.body, []byte(accessToken.Reveal())) {
		// A response that reflects the credential is never decoded or persisted.
		response.body = nil
		response.isCredentialReflected = true
	}
	return response, true, nil
}

// classifyReadResponse classifies a repeatable request's response; an interrupted 2xx body is retried.
func (client *Client) classifyReadResponse(operation string, response providerResponse) statusClassification {
	if !isSuccessStatus(response.statusCode) {
		return client.classifyErrorStatus(operation, response)
	}
	switch {
	case response.isBodyTooLarge:
		return statusClassification{outcome: statusOutcomeInvalid, failure: newFailure(operation, sdkgo.FailureResponseTooLarge, "Zoom response exceeds the configured size limit")}
	case response.isCredentialReflected:
		return statusClassification{outcome: statusOutcomeInvalid, failure: newFailure(operation, sdkgo.FailureProtocol, "Zoom response contained the connection credential")}
	case response.hasBodyReadFailed:
		return statusClassification{outcome: statusOutcomeRetry, failure: newFailure(operation, sdkgo.FailureTransport, "Zoom response was interrupted")}
	default:
		return statusClassification{outcome: statusOutcomeUsable}
	}
}

// classifyErrorStatus maps a non-2xx response, keeping only Zoom's numeric error code, never its text.
func (client *Client) classifyErrorStatus(operation string, response providerResponse) statusClassification {
	errorCode := zoomErrorCode(response.body)
	switch {
	case response.statusCode >= 300 && response.statusCode < 400:
		return statusClassification{
			outcome: statusOutcomeInvalid,
			failure: newFailure(operation, sdkgo.FailureProtocol, describeStatus("Zoom redirected the request", response.statusCode, "")),
		}
	case response.statusCode == http.StatusNotFound:
		return statusClassification{
			outcome: statusOutcomeNotFound, errorCode: errorCode,
			failure: newFailure(operation, sdkgo.FailureNotFound, describeStatus("Zoom reported the resource missing", response.statusCode, errorCode)),
		}
	case response.statusCode == http.StatusTooManyRequests:
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		if delay > maximumThrottleDelay {
			return statusClassification{
				outcome: statusOutcomeRejected, errorCode: errorCode,
				failure: newFailure(operation, sdkgo.FailureRateLimit, describeStatus("Zoom's rate limit resets later than one minute, as its daily limits do", response.statusCode, errorCode)),
			}
		}
		return statusClassification{
			outcome: statusOutcomeRetry, retryAfter: delay, errorCode: errorCode,
			failure: newFailure(operation, sdkgo.FailureRateLimit, describeStatus("Zoom rate limited the request", response.statusCode, errorCode)),
		}
	case response.statusCode == http.StatusRequestTimeout || response.statusCode >= http.StatusInternalServerError:
		return statusClassification{
			outcome: statusOutcomeRetry, retryAfter: providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now()),
			errorCode: errorCode,
			failure:   newFailure(operation, sdkgo.FailureAvailability, describeStatus("Zoom is temporarily unavailable", response.statusCode, errorCode)),
		}
	default:
		return statusClassification{
			outcome: statusOutcomeRejected, errorCode: errorCode,
			failure: newFailure(operation, statusFailureKind(response.statusCode), describeStatus("Zoom rejected the request", response.statusCode, errorCode)),
		}
	}
}

func (client *Client) receipt(call sdkgo.Call, objectID string, errorCode string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
	if errorCode != "" {
		receipt.Metadata = map[string]string{errorCodeMetadataKey: errorCode}
	}
	return receipt
}

func (observation *dispatchObservation) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSDone:          observation.recordDNSDone,
		ConnectDone:      observation.recordConnectDone,
		TLSHandshakeDone: observation.recordTLSHandshakeDone,
		GotConn:          observation.recordObtainedConnection,
	}
}

// isProvablyUndispatched requires a traced DNS, connect, or TLS failure and no obtained connection.
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

// zoomErrorCode returns Zoom's numeric error code from an error body, or "".
func zoomErrorCode(body []byte) string {
	for _, token := range providerhttp.ReadErrorTokens(body, zoomErrorCodePointers) {
		if _, err := strconv.ParseUint(token, 10, 32); err == nil {
			return token
		}
	}
	return ""
}

func describeStatus(message string, status int, errorCode string) string {
	if errorCode == "" {
		return fmt.Sprintf("%s with HTTP %d", message, status)
	}
	return fmt.Sprintf("%s with HTTP %d (code %s)", message, status, errorCode)
}

func statusFailureKind(status int) sdkgo.FailureKind {
	switch status {
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case http.StatusConflict, http.StatusPreconditionFailed:
		return sdkgo.FailureConflict
	default:
		return sdkgo.FailureProviderRejection
	}
}

func isSuccessStatus(status int) bool { return status >= 200 && status < 300 }

func meetingPath(meetingID int64) string {
	return "/meetings/" + strconv.FormatInt(meetingID, 10)
}

func newFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func failurePointer(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := newFailure(operation, kind, message)
	return &failure
}
