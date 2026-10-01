// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package notion searches, queries, reads, creates, and updates Notion pages
// and database rows as Dex connector Steps, through Notion's public REST API at
// https://api.notion.com/v1 with Notion-Version 2026-03-11.
//
// A Notion database is a container for one or more data sources, and its rows
// are pages whose parent is a data source. QueryDatabase and CreatePage accept
// a data source ID or a database ID; a database ID resolves to the database's
// only data source, and a database with several data sources selects defect.
//
// Notion accepts no idempotency key, so CreatePage never resends a request
// Notion may have received: an ambiguous outcome selects the uncertain branch,
// and the operation uses sync durability so Dex does not dispatch it twice.
// When Notion saves the page but answers 503 with the saved page's ID, the
// create reads that page and selects created. UpdatePageProperties sends
// absolute property values, so a repeated or duplicated update leaves the page
// in the same state, and it keeps async durability.
//
// Every failure names only the HTTP status and Notion's machine-readable error
// code, such as validation_error, and never Notion's message text.
package notion

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
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "notion"

	// NotionVersion is the Notion-Version header every request sends. The
	// connector's request and response shapes follow this version.
	NotionVersion = "2026-03-11"

	// readRequestTimeout bounds one read, so a read Step finishes inside its 30-second Execute timeout.
	readRequestTimeout = 20 * time.Second
	// readOperationDeadline bounds every request of one read Invoke.
	readOperationDeadline = 25 * time.Second
	// writeRequestTimeout outlasts Notion's roughly 55-second write deadline, so a slow save returns its 503.
	writeRequestTimeout = 60 * time.Second
	// writeOperationDeadline bounds a write Invoke inside its 90-second Execute timeout.
	writeOperationDeadline = 85 * time.Second

	notionVersionHeader   = "Notion-Version"
	requestIDHeader       = "X-Notion-Request-Id"
	maximumSafeTokenBytes = 128

	// statusServiceOverload is Notion's 529 service_overload, retried like a 429.
	statusServiceOverload = 529
	// rateLimitReasonRequestBlocked marks a 429 that no retry can recover.
	rateLimitReasonRequestBlocked = "public_api_request_blocked"
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient replaces the HTTP client used for Notion requests. The caller
// keeps ownership of client and its transport. Requests use a copy that never
// follows redirects. Without a client timeout, each read is bounded by 20
// seconds and each write by 60 seconds; a shorter client timeout bounds both.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Notion REST requests for one connection. It is
// safe for concurrent use by several Steps.
type Client struct {
	endpoint         string
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
	now              func() time.Time
}

// notionRequest is one Notion REST call below /v1.
type notionRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
	timeout time.Duration
}

// notionResponse is one bounded Notion response. The body is empty when it was oversized or unreadable.
type notionResponse struct {
	statusCode        int
	header            http.Header
	body              []byte
	isBodyTooLarge    bool
	hasBodyReadFailed bool
	reflectsSecret    bool
}

// exchangeResult reports one authenticated exchange; isDispatched false proves Notion received nothing.
type exchangeResult struct {
	response     notionResponse
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
	writeCommitted
	writeRetry
	writeRejected
	writeNotFound
	writeUncertain
	writeInvalid
	writeDefect
)

// writeClassification is the safe meaning of one write exchange; writeCommitted carries the saved object's ID.
type writeClassification struct {
	outcome             writeOutcome
	failure             sdkgo.Failure
	retryAfter          time.Duration
	committedResourceID string
}

// dispatchObservation records whether a request could have reached Notion.
type dispatchObservation struct {
	mutex                 sync.Mutex
	hasObtainedConnection bool
	hasFailedToConnect    bool
}

// New validates configuration and constructs an authenticated Notion client.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Notion endpoint: %w", err)
	}
	if config.MaxResponseBytes < 1 {
		return nil, fmt.Errorf("Notion maxResponseBytes must be positive")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Notion connector option is nil")
		}
		option(&dependencies)
	}
	return &Client{
		endpoint:         endpoint,
		httpClient:       providerhttp.NewProviderHTTPClient(dependencies.httpClient, writeRequestTimeout),
		credentials:      credentials,
		maxResponseBytes: config.MaxResponseBytes,
		now:              time.Now,
	}, nil
}

// Search returns the search Query bound to this client.
func (client *Client) Search() SearchOperation { return SearchOperation{client: client} }

// QueryDatabase returns the queryDatabase Query bound to this client.
func (client *Client) QueryDatabase() QueryDatabaseOperation {
	return QueryDatabaseOperation{client: client}
}

// GetPage returns the getPage Query bound to this client.
func (client *Client) GetPage() GetPageOperation { return GetPageOperation{client: client} }

// CreatePage returns the createPage Mutation bound to this client.
func (client *Client) CreatePage() CreatePageOperation { return CreatePageOperation{client: client} }

// UpdatePageProperties returns the updatePageProperties Mutation bound to this client.
func (client *Client) UpdatePageProperties() UpdatePagePropertiesOperation {
	return UpdatePagePropertiesOperation{client: client}
}

// startSession resolves the credential; the caller must call the returned cancel.
func (client *Client) startSession(call sdkgo.Call, operationID string, deadline time.Duration) (*operationSession, context.CancelFunc, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIToken.Reveal()) {
		return nil, nil, failurePointer(operationID, sdkgo.FailureAuthentication,
			"Notion connection credentials are unavailable; save the connection's API token again")
	}
	operationContext, cancel := context.WithTimeout(call.Context, deadline)
	return &operationSession{context: operationContext, call: call, credentials: credentials}, cancel, nil
}

func (client *Client) exchange(session *operationSession, request notionRequest) exchangeResult {
	var encodedPayload []byte
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return exchangeResult{err: errRequestNotBuilt}
		}
		encodedPayload = encoded
	}
	target := client.endpoint + "/v1" + request.path
	if len(request.query) > 0 {
		target += "?" + request.query.Encode()
	}
	timeout := request.timeout
	if timeout == 0 {
		timeout = readRequestTimeout
	}
	requestContext, cancel := context.WithTimeout(session.context, timeout)
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
	token := session.credentials.APIToken.Reveal()
	httpRequest.Header.Set("Authorization", "Bearer "+token)
	httpRequest.Header.Set(notionVersionHeader, NotionVersion)
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
	response := notionResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
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
	case bytes.Contains(response.body, []byte(token)):
		response.body, response.reflectsSecret = nil, true
	}
	return exchangeResult{response: response, isDispatched: true}
}

// classifyRead maps one read exchange to an outcome without choosing an operation branch.
func (client *Client) classifyRead(operationID string, subject string, result exchangeResult) readClassification {
	if errors.Is(result.err, errRequestNotBuilt) {
		return readClassification{outcome: readDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, "Notion request could not be built")}
	}
	if result.err != nil {
		return readClassification{outcome: readRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Notion "+subject+" is temporarily unavailable")}
	}
	response := result.response
	switch {
	case isBlockedRateLimit(response):
		return readClassification{outcome: readRejected, failure: blockedFailure(operationID, subject)}
	case response.statusCode == http.StatusTooManyRequests || response.statusCode == statusServiceOverload:
		return readClassification{outcome: readRetry, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureRateLimit, "Notion rate limited the "+subject)}
	case response.statusCode == http.StatusNotFound:
		return readClassification{outcome: readNotFound, failure: notFoundFailure(operationID, subject)}
	case isRetryableReadStatus(response.statusCode):
		return readClassification{outcome: readRetry, retryAfter: client.retryAfter(response), failure: statusFailure(operationID, "Notion "+subject+" is temporarily unavailable", response)}
	case response.statusCode >= 400 && response.statusCode < 500:
		return readClassification{outcome: readRejected, failure: rejectionFailure(operationID, subject, response)}
	case !isSuccessStatus(response.statusCode):
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, fmt.Sprintf("Notion returned unexpected HTTP %d for the %s", response.statusCode, subject))}
	case response.hasBodyReadFailed:
		return readClassification{outcome: readRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Notion "+subject+" response was interrupted")}
	case response.isBodyTooLarge:
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureResponseTooLarge, "Notion "+subject+" response exceeds the configured size limit")}
	case response.reflectsSecret:
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Notion "+subject+" response contains credential material")}
	default:
		return readClassification{outcome: readSucceeded}
	}
}

// classifyUnkeyedWrite retries only provably unwritten requests: undispatched, 429, and 529. Other ambiguity is uncertain.
func (client *Client) classifyUnkeyedWrite(operationID string, subject string, result exchangeResult) writeClassification {
	switch {
	case errors.Is(result.err, errRequestNotBuilt):
		return writeClassification{outcome: writeDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, "Notion request could not be built")}
	case result.err != nil && !result.isDispatched:
		return writeClassification{outcome: writeRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Notion could not be reached, so no "+subject+" was written")}
	case result.err != nil:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureTransport, "Notion "+subject+" outcome is unknown")}
	}
	response := result.response
	switch {
	case isSuccessStatus(response.statusCode) && response.hasBodyReadFailed:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureTransport, "Notion accepted the "+subject+" but its response was interrupted")}
	case isSuccessStatus(response.statusCode) && response.isBodyTooLarge:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureResponseTooLarge, "Notion accepted the "+subject+" but its response exceeds the configured size limit")}
	case isSuccessStatus(response.statusCode) && response.reflectsSecret:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureProtocol, "Notion accepted the "+subject+" but its response contains credential material")}
	case isSuccessStatus(response.statusCode):
		return writeClassification{outcome: writeAccepted}
	case isBlockedRateLimit(response):
		return writeClassification{outcome: writeRejected, failure: blockedFailure(operationID, subject)}
	case response.statusCode == http.StatusTooManyRequests || response.statusCode == statusServiceOverload:
		return writeClassification{outcome: writeRetry, retryAfter: client.retryAfter(response),
			failure: newFailure(operationID, sdkgo.FailureRateLimit, "Notion rate limited the "+subject+" before writing it")}
	case response.statusCode == http.StatusServiceUnavailable && readCommittedResourceID(response.body) != "":
		return writeClassification{outcome: writeCommitted, committedResourceID: readCommittedResourceID(response.body),
			failure: newFailure(operationID, sdkgo.FailureAvailability, "Notion saved the "+subject+" but could not build its response in time")}
	case response.statusCode == http.StatusNotFound:
		return writeClassification{outcome: writeNotFound, failure: notFoundFailure(operationID, subject)}
	case isConclusiveRejectionStatus(response.statusCode):
		return writeClassification{outcome: writeRejected, failure: rejectionFailure(operationID, subject, response)}
	default:
		return writeClassification{outcome: writeUncertain,
			failure: statusFailure(operationID, fmt.Sprintf("Notion %s outcome is unknown after HTTP %d", subject, response.statusCode), response)}
	}
}

// classifyRepeatableWrite maps a write whose repetition leaves the same state: every ambiguity is Retry.
func (client *Client) classifyRepeatableWrite(operationID string, subject string, result exchangeResult) writeClassification {
	switch {
	case errors.Is(result.err, errRequestNotBuilt):
		return writeClassification{outcome: writeDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, "Notion request could not be built")}
	case result.err != nil:
		return writeClassification{outcome: writeRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Notion "+subject+" is temporarily unavailable")}
	}
	response := result.response
	switch {
	case isSuccessStatus(response.statusCode) && response.hasBodyReadFailed:
		return writeClassification{outcome: writeRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Notion "+subject+" response was interrupted")}
	case isSuccessStatus(response.statusCode) && response.isBodyTooLarge:
		return writeClassification{outcome: writeInvalid, failure: newFailure(operationID, sdkgo.FailureResponseTooLarge, "Notion applied the "+subject+" but its response exceeds the configured size limit")}
	case isSuccessStatus(response.statusCode) && response.reflectsSecret:
		return writeClassification{outcome: writeInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Notion "+subject+" response contains credential material")}
	case isSuccessStatus(response.statusCode):
		return writeClassification{outcome: writeAccepted}
	case isBlockedRateLimit(response):
		return writeClassification{outcome: writeRejected, failure: blockedFailure(operationID, subject)}
	case response.statusCode == http.StatusTooManyRequests || response.statusCode == statusServiceOverload:
		return writeClassification{outcome: writeRetry, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureRateLimit, "Notion rate limited the "+subject)}
	case response.statusCode == http.StatusConflict || isRetryableReadStatus(response.statusCode):
		return writeClassification{outcome: writeRetry, retryAfter: client.retryAfter(response), failure: statusFailure(operationID, "Notion "+subject+" is temporarily unavailable", response)}
	case response.statusCode == http.StatusNotFound:
		return writeClassification{outcome: writeNotFound, failure: notFoundFailure(operationID, subject)}
	case response.statusCode >= 400 && response.statusCode < 500:
		return writeClassification{outcome: writeRejected, failure: rejectionFailure(operationID, subject, response)}
	default:
		return writeClassification{outcome: writeInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, fmt.Sprintf("Notion returned unexpected HTTP %d for the %s", response.statusCode, subject))}
	}
}

func (client *Client) retryAfter(response notionResponse) time.Duration {
	return providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
}

func (client *Client) receipt(session *operationSession, response notionResponse, objectID string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: session.call.ID, IdempotencyKey: session.call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
	if value := response.header.Get(requestIDHeader); isSafeProviderToken(value) {
		receipt.ProviderRequestID = value
	} else if requestID := readErrorToken(response.body, "/request_id"); requestID != "" {
		receipt.ProviderRequestID = requestID
	}
	return receipt
}

// readErrorToken returns one machine-readable token of a Notion error body; message text is never read.
func readErrorToken(body []byte, pointer string) string {
	tokens := providerhttp.ReadErrorTokens(body, []string{pointer})
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

// readCommittedResourceID returns the saved object's ID from a 503 body, or "" when Notion names none.
func readCommittedResourceID(body []byte) string {
	id, err := ParseID(readErrorToken(body, "/additional_data/committed_resource_id"))
	if err != nil {
		return ""
	}
	return id
}

func isBlockedRateLimit(response notionResponse) bool {
	return response.statusCode == http.StatusTooManyRequests &&
		readErrorToken(response.body, "/additional_data/rate_limit_reason") == rateLimitReasonRequestBlocked
}

func blockedFailure(operationID string, subject string) sdkgo.Failure {
	return newFailure(operationID, sdkgo.FailureProviderRejection,
		"Notion blocked this connection's API access for the "+subject+" ("+rateLimitReasonRequestBlocked+"); retrying cannot help")
}

// rejectionFailure describes a conclusive Notion rejection by status and error code, never by provider text.
func rejectionFailure(operationID string, subject string, response notionResponse) sdkgo.Failure {
	message := fmt.Sprintf("Notion rejected the %s with HTTP %d", subject, response.statusCode)
	if code := readErrorToken(response.body, "/code"); code != "" {
		message += " (" + code + ")"
	}
	switch response.statusCode {
	case http.StatusUnauthorized:
		message += "; the API token is invalid, expired, or revoked"
	case http.StatusForbidden:
		message += "; the connection lacks a capability this request needs, or the workspace reached a plan limit"
	case http.StatusBadRequest:
		message += "; check every property name and that each value matches its property type"
	}
	return newFailure(operationID, statusFailureKind(response.statusCode), message)
}

// statusFailure names the status and error code of a retryable or ambiguous response.
func statusFailure(operationID string, message string, response notionResponse) sdkgo.Failure {
	if code := readErrorToken(response.body, "/code"); code != "" {
		message += " (" + code + ")"
	}
	kind := sdkgo.FailureAvailability
	if response.statusCode == http.StatusConflict {
		kind = sdkgo.FailureConflict
	}
	return newFailure(operationID, kind, message)
}

func notFoundFailure(operationID string, subject string) sdkgo.Failure {
	return newFailure(operationID, sdkgo.FailureNotFound,
		"the Notion object for the "+subject+" was not found or is not shared with the connection; add the connection from the page's ••• menu > Connections")
}

func statusFailureKind(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case http.StatusBadRequest:
		return sdkgo.FailureValidation
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden:
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

// isConclusiveRejectionStatus reports a 4xx other than 408, 409, and 429: Notion refused before writing.
func isConclusiveRejectionStatus(statusCode int) bool {
	return statusCode >= 400 && statusCode < 500 && statusCode != http.StatusRequestTimeout &&
		statusCode != http.StatusConflict && statusCode != http.StatusTooManyRequests
}

// isRetryableReadStatus reports a status a repeated read or absolute write can safely wait out.
func isRetryableReadStatus(statusCode int) bool {
	return statusCode == http.StatusRequestTimeout || statusCode == http.StatusConflict ||
		statusCode == http.StatusTooManyRequests || statusCode >= 500
}

// isSafeProviderToken accepts a bounded printable ASCII value such as a request ID.
func isSafeProviderToken(value string) bool {
	if value == "" || len(value) > maximumSafeTokenBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] <= ' ' || value[index] > '~' {
			return false
		}
	}
	return true
}

func newFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operationID, Message: message}
}

func failurePointer(operationID string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := newFailure(operationID, kind, message)
	return &failure
}

func validationFailure(operationID string, err error) *sdkgo.Failure {
	return failurePointer(operationID, sdkgo.FailureValidation, err.Error())
}

var errRequestNotBuilt = errors.New("Notion request could not be built")

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
