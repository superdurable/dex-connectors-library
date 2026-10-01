// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package asana lists, reads, creates, and updates Asana tasks and comments on them as Dex connector
// Steps, through the Asana REST API at https://app.asana.com/api/1.0.
//
// Asana accepts no idempotency key, so CreateTask and AddComment never resend a request Asana may have
// received: an ambiguous outcome selects the uncertain branch, and both default to sync durability.
// UpdateTask writes only absolute values, such as completed true or a section membership, so a repeated
// or concurrent update converges on the same task and keeps the async default.
package asana

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
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "asana"

	// operationDeadline keeps every request of one Invoke inside the 30-second Execute timeout.
	operationDeadline = 25 * time.Second
	// requestTimeout bounds one exchange, so a hung write selects uncertain before Dex times the Step out.
	requestTimeout = 20 * time.Second
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient replaces the HTTP client used for Asana API requests. The caller keeps ownership of client
// and its transport. Requests use a copy that never follows redirects and is bounded by 20 seconds, so a hung
// write selects uncertain before Dex's 30-second Execute timeout.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Asana REST requests for one connection. It is safe for concurrent use by
// several Steps.
type Client struct {
	endpoint         string
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
	now              func() time.Time
}

// asanaRequest is one Asana REST call below the API base URL, such as GET /tasks/{task_gid}.
type asanaRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

// asanaResponse is one bounded Asana response. The body is empty when it was oversized or unreadable.
type asanaResponse struct {
	statusCode        int
	header            http.Header
	body              []byte
	isBodyTooLarge    bool
	hasBodyReadFailed bool
	reflectsSecret    bool
}

// exchangeResult reports one authenticated exchange; isDispatched false proves Asana received nothing.
type exchangeResult struct {
	response     asanaResponse
	isDispatched bool
	err          error
}

// operationSession carries the resolved credential for one Invoke.
type operationSession struct {
	context     context.Context
	call        sdkgo.Call
	credentials Credentials
}

type readOutcome uint8

const (
	readSucceeded readOutcome = iota + 1
	readNotFound
	readRetry
	readRejected
	readInvalid
	readDefect
)

// readClassification is the safe meaning of one read exchange.
type readClassification struct {
	outcome    readOutcome
	failure    sdkgo.Failure
	retryAfter time.Duration
}

type writeOutcome uint8

const (
	writeAccepted writeOutcome = iota + 1
	writeRetry
	writeRejected
	writeNotFound
	writeUncertain
	writeInvalid
	writeDefect
)

// writeClassification is the safe meaning of one write exchange.
type writeClassification struct {
	outcome    writeOutcome
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// dispatchObservation records whether a request could have reached Asana.
type dispatchObservation struct {
	mutex                 sync.Mutex
	hasObtainedConnection bool
	hasFailedToConnect    bool
}

// dataEnvelope is Asana's {"data": ...} request and response wrapper.
type dataEnvelope[T any] struct {
	Data T `json:"data"`
}

// New validates configuration and constructs an authenticated Asana client.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Asana endpoint: %w", err)
	}
	if config.MaxResponseBytes < 1 {
		return nil, fmt.Errorf("Asana maxResponseBytes must be positive")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Asana connector option is nil")
		}
		option(&dependencies)
	}
	return &Client{
		endpoint:         endpoint,
		httpClient:       providerhttp.NewProviderHTTPClient(dependencies.httpClient, requestTimeout),
		credentials:      credentials,
		maxResponseBytes: config.MaxResponseBytes, now: time.Now,
	}, nil
}

// ListTasks returns the listTasks Query bound to this client.
func (client *Client) ListTasks() ListTasksOperation { return ListTasksOperation{client: client} }

// GetTask returns the getTask Query bound to this client.
func (client *Client) GetTask() GetTaskOperation { return GetTaskOperation{client: client} }

// CreateTask returns the createTask Mutation bound to this client.
func (client *Client) CreateTask() CreateTaskOperation { return CreateTaskOperation{client: client} }

// UpdateTask returns the updateTask Mutation bound to this client.
func (client *Client) UpdateTask() UpdateTaskOperation { return UpdateTaskOperation{client: client} }

// AddComment returns the addComment Mutation bound to this client.
func (client *Client) AddComment() AddCommentOperation { return AddCommentOperation{client: client} }

// startSession resolves the credential, reread before every call; the caller must call the returned cancel.
func (client *Client) startSession(call sdkgo.Call, operationID string) (*operationSession, context.CancelFunc, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return nil, nil, failurePointer(operationID, sdkgo.FailureAuthentication, "Asana connection credentials are unavailable; save a personal access token for the connection")
	}
	operationContext, cancel := context.WithTimeout(call.Context, operationDeadline)
	return &operationSession{context: operationContext, call: call, credentials: credentials}, cancel, nil
}

// exchange sends one request. A personal access token cannot be refreshed, so a 401 is never resent.
func (client *Client) exchange(session *operationSession, request asanaRequest) exchangeResult {
	var encodedPayload []byte
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return exchangeResult{err: errRequestNotBuilt}
		}
		encodedPayload = encoded
	}
	target := client.endpoint + request.path
	if len(request.query) > 0 {
		target += "?" + request.query.Encode()
	}
	requestContext, cancel := context.WithTimeout(session.context, requestTimeout)
	defer cancel()
	observation := &dispatchObservation{}
	requestContext = httptrace.WithClientTrace(requestContext, observation.clientTrace())
	var body io.Reader
	if encodedPayload != nil {
		body = bytes.NewReader(encodedPayload)
	}
	httpRequest, err := http.NewRequestWithContext(requestContext, request.method, target, body)
	if err != nil {
		return exchangeResult{err: errRequestNotBuilt}
	}
	accessToken := session.credentials.AccessToken.Reveal()
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken)
	httpRequest.Header.Set("Accept", "application/json")
	if encodedPayload != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return exchangeResult{isDispatched: !observation.isProvablyUndispatched(), err: err}
	}
	defer func() {
		// The body is fully read or bounded below; a close failure cannot change the classified response.
		_ = httpResponse.Body.Close()
	}()
	response := asanaResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
	limit := client.maxResponseBytes
	if !isSuccessStatus(httpResponse.StatusCode) {
		limit = providerhttp.MaxErrorBodyBytes
	}
	response.body, err = providerhttp.ReadBoundedBody(httpResponse.Body, limit)
	switch {
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		response.body, response.isBodyTooLarge = nil, true
	case err != nil:
		response.body, response.hasBodyReadFailed = nil, true
	case bytes.Contains(response.body, []byte(accessToken)):
		response.body, response.reflectsSecret = nil, true
	}
	return exchangeResult{response: response, isDispatched: true}
}

// classifyRead maps one read exchange to an outcome without choosing an operation branch.
func (client *Client) classifyRead(operationID string, subject string, result exchangeResult) readClassification {
	if errors.Is(result.err, errRequestNotBuilt) {
		return readClassification{outcome: readDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, "Asana request could not be built")}
	}
	if result.err != nil {
		return readClassification{outcome: readRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Asana "+subject+" is temporarily unavailable")}
	}
	response := result.response
	switch {
	case response.statusCode == http.StatusNotFound:
		return readClassification{outcome: readNotFound, failure: newFailure(operationID, sdkgo.FailureNotFound, "Asana "+subject+" was not found or is not visible to the connection")}
	case response.statusCode == http.StatusTooManyRequests:
		return readClassification{outcome: readRetry, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureRateLimit, "Asana rate limited the "+subject)}
	case isRetryableStatus(response.statusCode):
		return readClassification{outcome: readRetry, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureAvailability, "Asana "+subject+" is temporarily unavailable")}
	case response.statusCode >= 400 && response.statusCode < 500:
		return readClassification{outcome: readRejected, failure: rejectionFailure(operationID, subject, response.statusCode)}
	case !isSuccessStatus(response.statusCode):
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, fmt.Sprintf("Asana returned unexpected HTTP %d for the %s", response.statusCode, subject))}
	case response.hasBodyReadFailed:
		return readClassification{outcome: readRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Asana "+subject+" response was interrupted")}
	case response.isBodyTooLarge:
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureResponseTooLarge, "Asana "+subject+" response exceeds the configured size limit")}
	case response.reflectsSecret:
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Asana "+subject+" response contains credential material")}
	default:
		return readClassification{outcome: readSucceeded}
	}
}

// classifyUnkeyedWrite retries only a 429 or an undispatched request, when Asana cannot have written.
func (client *Client) classifyUnkeyedWrite(operationID string, subject string, result exchangeResult) writeClassification {
	switch {
	case errors.Is(result.err, errRequestNotBuilt):
		return writeClassification{outcome: writeDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, "Asana request could not be built")}
	case result.err != nil && !result.isDispatched:
		return writeClassification{outcome: writeRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Asana could not be reached, so no "+subject+" was written")}
	case result.err != nil:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureTransport, "Asana "+subject+" outcome is unknown")}
	}
	response := result.response
	switch {
	case isSuccessStatus(response.statusCode) && response.hasBodyReadFailed:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureTransport, "Asana accepted the "+subject+" but its response was interrupted")}
	case isSuccessStatus(response.statusCode) && response.isBodyTooLarge:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureResponseTooLarge, "Asana accepted the "+subject+" but its response exceeds the configured size limit")}
	case isSuccessStatus(response.statusCode) && response.reflectsSecret:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureProtocol, "Asana accepted the "+subject+" but its response contains credential material")}
	case isSuccessStatus(response.statusCode):
		return writeClassification{outcome: writeAccepted}
	case response.statusCode == http.StatusTooManyRequests:
		return writeClassification{outcome: writeRetry, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureRateLimit, "Asana rate limited the "+subject+" before writing it")}
	case response.statusCode == http.StatusNotFound:
		return writeClassification{outcome: writeNotFound, failure: rejectionFailure(operationID, subject, response.statusCode)}
	case isConclusiveRejectionStatus(response.statusCode):
		return writeClassification{outcome: writeRejected, failure: rejectionFailure(operationID, subject, response.statusCode)}
	default:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureAvailability, fmt.Sprintf("Asana %s outcome is unknown after HTTP %d", subject, response.statusCode))}
	}
}

// classifyRepeatableWrite retries every ambiguous outcome, because the request sets absolute values that
// Asana may apply again without changing the result.
func (client *Client) classifyRepeatableWrite(operationID string, subject string, result exchangeResult) writeClassification {
	switch {
	case errors.Is(result.err, errRequestNotBuilt):
		return writeClassification{outcome: writeDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, "Asana request could not be built")}
	case result.err != nil:
		return writeClassification{outcome: writeRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Asana "+subject+" outcome is unknown, so the update is sent again")}
	}
	response := result.response
	switch {
	case isSuccessStatus(response.statusCode) && response.hasBodyReadFailed:
		return writeClassification{outcome: writeRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Asana "+subject+" response was interrupted, so the update is sent again")}
	case isSuccessStatus(response.statusCode) && response.isBodyTooLarge:
		return writeClassification{outcome: writeInvalid, failure: newFailure(operationID, sdkgo.FailureResponseTooLarge, "Asana "+subject+" response exceeds the configured size limit")}
	case isSuccessStatus(response.statusCode) && response.reflectsSecret:
		return writeClassification{outcome: writeInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Asana "+subject+" response contains credential material")}
	case isSuccessStatus(response.statusCode):
		return writeClassification{outcome: writeAccepted}
	case response.statusCode == http.StatusTooManyRequests:
		return writeClassification{outcome: writeRetry, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureRateLimit, "Asana rate limited the "+subject)}
	case response.statusCode == http.StatusNotFound:
		return writeClassification{outcome: writeNotFound, failure: rejectionFailure(operationID, subject, response.statusCode)}
	case isConclusiveRejectionStatus(response.statusCode):
		return writeClassification{outcome: writeRejected, failure: rejectionFailure(operationID, subject, response.statusCode)}
	case isRetryableStatus(response.statusCode):
		return writeClassification{outcome: writeRetry, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureAvailability, "Asana "+subject+" is temporarily unavailable")}
	default:
		return writeClassification{outcome: writeInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, fmt.Sprintf("Asana returned unexpected HTTP %d for the %s", response.statusCode, subject))}
	}
}

func (client *Client) retryAfter(response asanaResponse) time.Duration {
	return providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
}

// receipt records the call; Asana documents no request ID header, so ProviderRequestID stays empty.
func (client *Client) receipt(session *operationSession, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: session.call.ID, IdempotencyKey: session.call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
}

// rejectionFailure describes a conclusive Asana rejection by status only; Asana's error messages are
// free text that can repeat task content, so they are never read.
func rejectionFailure(operationID string, subject string, statusCode int) sdkgo.Failure {
	return newFailure(operationID, statusFailureKind(statusCode), fmt.Sprintf("Asana rejected the %s with HTTP %d", subject, statusCode))
}

func statusFailureKind(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case http.StatusBadRequest:
		return sdkgo.FailureValidation
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden, http.StatusPaymentRequired:
		// Asana answers 402 for a feature above the workspace's plan, an entitlement and not spent credit.
		return sdkgo.FailureAuthorization
	case http.StatusNotFound:
		return sdkgo.FailureNotFound
	case http.StatusConflict:
		return sdkgo.FailureConflict
	case http.StatusTooManyRequests:
		return sdkgo.FailureRateLimit
	default:
		return sdkgo.FailureProviderRejection
	}
}

func isSuccessStatus(statusCode int) bool { return statusCode >= 200 && statusCode < 300 }

// isConclusiveRejectionStatus reports a 4xx other than 408 and 429: Asana refused before changing anything.
func isConclusiveRejectionStatus(statusCode int) bool {
	return statusCode >= 400 && statusCode < 500 && statusCode != http.StatusRequestTimeout && statusCode != http.StatusTooManyRequests
}

func isRetryableStatus(statusCode int) bool {
	return statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooManyRequests || statusCode >= 500
}

func newFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operationID, Message: message}
}

func failurePointer(operationID string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := newFailure(operationID, kind, message)
	return &failure
}

// joinPath builds an API path from escaped segments, such as /tasks/{task_gid}/stories.
func joinPath(segments ...string) string {
	var builder strings.Builder
	for _, segment := range segments {
		builder.WriteByte('/')
		builder.WriteString(url.PathEscape(segment))
	}
	return builder.String()
}

var errRequestNotBuilt = errors.New("Asana request could not be built")

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
