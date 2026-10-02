// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package teams posts Microsoft Teams channel messages, thread replies, and
// chat messages, and reads a channel message's replies, as Dex connector
// Steps through Microsoft Graph v1.0 with delegated OAuth.
//
// Microsoft Graph accepts no idempotency key or client-supplied message ID
// for chatMessage, so every post runs with sync Execute durability and records
// a Dex heartbeat checkpoint before it is dispatched. An attempt that finds
// the checkpoint never posts again: a channel post or thread reply looks for
// its own message among the newest messages it would have created and reports
// it as sent, and otherwise selects uncertain. A chat message cannot be read
// back with the connection's permissions, so it selects uncertain.
//
// Graph allows application permissions for channel posts only for message
// migration, so the connector posts as the signed-in user.
package teams

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
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "microsoft-teams"

	// operationDeadline keeps every request of one Invoke inside the 30-second Execute timeout.
	operationDeadline = 25 * time.Second
	// requestTimeout bounds one exchange, so a hung post is reconciled before Dex times the Step out.
	requestTimeout = 20 * time.Second

	requestIDHeader       = "request-id"
	clientRequestIDHeader = "client-request-id"
	maximumSafeTokenBytes = 128
)

var (
	teamIDPattern      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	channelIDPattern   = regexp.MustCompile(`^19:[A-Za-z0-9._-]{1,256}@thread\.[A-Za-z0-9]{1,32}$`)
	chatIDPattern      = regexp.MustCompile(`^19:[A-Za-z0-9._-]{1,256}@[A-Za-z0-9.]{1,64}$`)
	messageIDPattern   = regexp.MustCompile(`^[0-9]{1,20}$`)
	errRequestNotBuilt = errors.New("Microsoft Graph request could not be built")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient replaces the HTTP client used for Microsoft Graph and
// Microsoft identity platform token requests. The caller keeps ownership of
// client and its transport. Graph requests use a copy that never follows
// redirects and is bounded by 20 seconds unless client sets its own Timeout,
// so a hung post is reconciled before Dex's 30-second Execute timeout.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Microsoft Graph Teams requests for one
// connection. It is safe for concurrent use by several Steps.
type Client struct {
	endpoint         string
	endpointURL      *url.URL
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	refreshDriver    sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes int64
	maxMessageBytes  int64
	now              func() time.Time
}

// graphRequest is one Microsoft Graph call below the endpoint, or one validated absolute next-page link.
type graphRequest struct {
	method      string
	path        string
	query       url.Values
	absoluteURL string
	payload     any
}

// graphResponse is one bounded response. The body is empty when it was oversized, unreadable, or reflected the token.
type graphResponse struct {
	statusCode        int
	header            http.Header
	body              []byte
	isBodyTooLarge    bool
	hasBodyReadFailed bool
	reflectsSecret    bool
}

// exchangeResult reports one authenticated exchange; isDispatched false proves Graph received nothing.
type exchangeResult struct {
	response     graphResponse
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
	// writeAccepted is a 2xx whose body was read in full.
	writeAccepted writeOutcome = iota + 1
	// writeNotApplied is a 429 or an undispatched request: Graph cannot have stored the message.
	writeNotApplied
	// writeRejected is a conclusive 4xx: Graph refused before storing anything.
	writeRejected
	// writeAmbiguous may have been stored: a lost response, 3xx, 408, 5xx, or an unusable 2xx body.
	writeAmbiguous
	writeDefect
)

// writeClassification is the safe meaning of one write exchange.
type writeClassification struct {
	outcome    writeOutcome
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// dispatchObservation records whether a request could have reached Graph.
type dispatchObservation struct {
	mutex                 sync.Mutex
	hasObtainedConnection bool
	hasFailedToConnect    bool
}

// New validates configuration and constructs an authenticated Microsoft Teams client. A blank
// configuration field takes its manifest default. The credential provider is consulted before
// every request; a provider that supports refresh renews the access token with the Microsoft
// identity platform before it expires.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Microsoft Teams endpoint: %w", err)
	}
	endpointURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("Microsoft Teams endpoint is invalid")
	}
	if config.MaxResponseBytes < 1 {
		return nil, fmt.Errorf("Microsoft Teams maxResponseBytes must be positive")
	}
	if config.MaxMessageBytes < 1 {
		return nil, fmt.Errorf("Microsoft Teams maxMessageBytes must be positive")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Microsoft Teams connector option is nil")
		}
		option(&dependencies)
	}
	return &Client{
		endpoint: endpoint, endpointURL: endpointURL,
		httpClient:       providerhttp.NewProviderHTTPClient(dependencies.httpClient, requestTimeout),
		credentials:      credentials,
		refreshDriver:    NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes: config.MaxResponseBytes, maxMessageBytes: config.MaxMessageBytes, now: time.Now,
	}, nil
}

// ListThreadReplies returns the listThreadReplies Query bound to this client.
func (client *Client) ListThreadReplies() ListThreadRepliesOperation {
	return ListThreadRepliesOperation{client: client}
}

// PostChannelMessage returns the postChannelMessage Mutation bound to this client.
func (client *Client) PostChannelMessage() PostChannelMessageOperation {
	return PostChannelMessageOperation{client: client}
}

// PostThreadReply returns the postThreadReply Mutation bound to this client.
func (client *Client) PostThreadReply() PostThreadReplyOperation {
	return PostThreadReplyOperation{client: client}
}

// PostChatMessage returns the postChatMessage Mutation bound to this client.
func (client *Client) PostChatMessage() PostChatMessageOperation {
	return PostChatMessageOperation{client: client}
}

// startSession resolves the credential; the caller must call the returned cancel.
func (client *Client) startSession(call sdkgo.Call, operationID string) (*operationSession, context.CancelFunc, *sdkgo.Failure) {
	operationContext, cancel := context.WithTimeout(call.Context, operationDeadline)
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		cancel()
		return nil, nil, failurePointer(operationID, sdkgo.FailureAuthentication, "Microsoft Teams connection credentials are unavailable; reconnect the connection")
	}
	return &operationSession{context: operationContext, call: call, credentials: credentials}, cancel, nil
}

// exchange refreshes and resends once after a 401; Graph rejects an unauthenticated request before acting.
func (client *Client) exchange(session *operationSession, request graphRequest) exchangeResult {
	var encodedPayload []byte
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return exchangeResult{err: errRequestNotBuilt}
		}
		encodedPayload = encoded
	}
	target := client.requestURL(request)
	for attempt := 0; ; attempt++ {
		result := client.exchangeOnce(session, request.method, target, encodedPayload)
		if result.err != nil || result.response.statusCode != http.StatusUnauthorized || attempt > 0 {
			return result
		}
		if _, supportsRejection := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !supportsRejection {
			return result
		}
		replacement, err := sdkgo.ResolveCredentialAfterRejection(session.call.Context, client.credentials, session.call, client.refreshDriver)
		if err != nil || validateResolvedCredentials(replacement) != nil {
			return result
		}
		session.credentials = replacement
	}
}

func (client *Client) requestURL(request graphRequest) string {
	if request.absoluteURL != "" {
		return request.absoluteURL
	}
	target := client.endpoint + request.path
	if len(request.query) > 0 {
		target += "?" + request.query.Encode()
	}
	return target
}

func (client *Client) exchangeOnce(session *operationSession, method string, target string, payload []byte) exchangeResult {
	requestContext, cancel := context.WithTimeout(session.context, requestTimeout)
	defer cancel()
	observation := &dispatchObservation{}
	requestContext = httptrace.WithClientTrace(requestContext, observation.clientTrace())
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(requestContext, method, target, body)
	if err != nil {
		return exchangeResult{err: errRequestNotBuilt}
	}
	accessToken := session.credentials.AccessToken.Reveal()
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", "application/json")
	// Graph logs this GUID for support correlation; it is not an idempotency key.
	request.Header.Set(clientRequestIDHeader, string(session.call.ID))
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	httpResponse, err := client.httpClient.Do(request)
	if err != nil {
		return exchangeResult{isDispatched: !observation.isProvablyUndispatched(), err: err}
	}
	defer func() {
		// The body is fully read or bounded below; a close failure cannot change the classified response.
		_ = httpResponse.Body.Close()
	}()
	response := graphResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
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
		return readClassification{outcome: readDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, "Microsoft Graph request could not be built")}
	}
	if result.err != nil {
		return readClassification{outcome: readRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Microsoft Graph "+subject+" is temporarily unavailable")}
	}
	response := result.response
	switch {
	case response.statusCode == http.StatusNotFound:
		return readClassification{outcome: readNotFound, failure: rejectionFailure(operationID, subject, response)}
	case response.statusCode == http.StatusTooManyRequests:
		return readClassification{outcome: readRetry, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureRateLimit, "Microsoft Graph throttled the "+subject)}
	case response.statusCode == http.StatusRequestTimeout || (response.statusCode >= 500 && response.statusCode != http.StatusNotImplemented):
		return readClassification{outcome: readRetry, retryAfter: client.retryAfter(response),
			failure: newFailure(operationID, sdkgo.FailureAvailability, fmt.Sprintf("Microsoft Graph %s is temporarily unavailable (HTTP %d%s)", subject, response.statusCode, errorCodeSuffix(response)))}
	case response.statusCode >= 400:
		return readClassification{outcome: readRejected, failure: rejectionFailure(operationID, subject, response)}
	case !isSuccessStatus(response.statusCode):
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, fmt.Sprintf("Microsoft Graph returned unexpected HTTP %d for the %s", response.statusCode, subject))}
	case response.hasBodyReadFailed:
		return readClassification{outcome: readRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Microsoft Graph "+subject+" response was interrupted")}
	case response.isBodyTooLarge:
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureResponseTooLarge, "Microsoft Graph "+subject+" response exceeds the configured size limit")}
	case response.reflectsSecret:
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Microsoft Graph "+subject+" response contains credential material")}
	default:
		return readClassification{outcome: readSucceeded}
	}
}

// classifyWrite separates posts Graph cannot have stored from conclusive and ambiguous outcomes.
func (client *Client) classifyWrite(operationID string, subject string, result exchangeResult) writeClassification {
	switch {
	case errors.Is(result.err, errRequestNotBuilt):
		return writeClassification{outcome: writeDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, "Microsoft Graph request could not be built")}
	case result.err != nil && !result.isDispatched:
		return writeClassification{outcome: writeNotApplied, failure: newFailure(operationID, sdkgo.FailureTransport, "Microsoft Graph could not be reached, so no "+subject+" was sent")}
	case result.err != nil:
		return writeClassification{outcome: writeAmbiguous, failure: newFailure(operationID, sdkgo.FailureTransport, "Microsoft Graph "+subject+" outcome is unknown")}
	}
	response := result.response
	switch {
	case isSuccessStatus(response.statusCode) && response.hasBodyReadFailed:
		return writeClassification{outcome: writeAmbiguous, failure: newFailure(operationID, sdkgo.FailureTransport, "Microsoft Graph accepted the "+subject+" but its response was interrupted")}
	case isSuccessStatus(response.statusCode) && response.isBodyTooLarge:
		return writeClassification{outcome: writeAmbiguous, failure: newFailure(operationID, sdkgo.FailureResponseTooLarge, "Microsoft Graph accepted the "+subject+" but its response exceeds the configured size limit")}
	case isSuccessStatus(response.statusCode) && response.reflectsSecret:
		return writeClassification{outcome: writeAmbiguous, failure: newFailure(operationID, sdkgo.FailureProtocol, "Microsoft Graph accepted the "+subject+" but its response contains credential material")}
	case isSuccessStatus(response.statusCode):
		return writeClassification{outcome: writeAccepted}
	case response.statusCode == http.StatusTooManyRequests:
		return writeClassification{outcome: writeNotApplied, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureRateLimit, "Microsoft Graph throttled the "+subject+" before storing it")}
	case isConclusiveRejectionStatus(response.statusCode):
		return writeClassification{outcome: writeRejected, failure: rejectionFailure(operationID, subject, response)}
	default:
		return writeClassification{outcome: writeAmbiguous, retryAfter: client.retryAfter(response),
			failure: newFailure(operationID, sdkgo.FailureAvailability, fmt.Sprintf("Microsoft Graph %s outcome is unknown after HTTP %d%s", subject, response.statusCode, errorCodeSuffix(response)))}
	}
}

func (client *Client) retryAfter(response graphResponse) time.Duration {
	return providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
}

func (client *Client) receipt(session *operationSession, response graphResponse, objectID string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: session.call.ID, IdempotencyKey: session.call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
	if response.header != nil {
		if value := response.header.Get(requestIDHeader); isSafeProviderToken(value) {
			receipt.ProviderRequestID = value
		}
	}
	return receipt
}

// isGraphLink reports whether link is an absolute URL on the configured Graph endpoint's scheme and host.
func (client *Client) isGraphLink(link *url.URL) bool {
	return link.Scheme == client.endpointURL.Scheme && strings.EqualFold(link.Host, client.endpointURL.Host) &&
		link.User == nil && link.Fragment == "" && link.Opaque == ""
}

// rejectionFailure describes a conclusive rejection by HTTP status and Graph error code, never by message text.
func rejectionFailure(operationID string, subject string, response graphResponse) sdkgo.Failure {
	message := fmt.Sprintf("Microsoft Graph rejected the %s with HTTP %d%s", subject, response.statusCode, errorCodeSuffix(response))
	switch response.statusCode {
	case http.StatusUnauthorized:
		message += "; reconnect the connection"
	case http.StatusForbidden:
		message += "; the connected account must belong to the team, channel, or chat, and the app needs every requested Graph permission, with administrator consent for ChannelMessage.Read.All"
	}
	return newFailure(operationID, statusFailureKind(response.statusCode), message)
}

// errorCodeSuffix names Graph's machine-readable error.code, such as Forbidden, when it is a safe token.
func errorCodeSuffix(response graphResponse) string {
	tokens := providerhttp.ReadErrorTokens(response.body, []string{"/error/code"})
	if len(tokens) == 0 {
		return ""
	}
	return " " + tokens[0]
}

func statusFailureKind(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity:
		return sdkgo.FailureValidation
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case http.StatusNotFound, http.StatusGone:
		return sdkgo.FailureNotFound
	case http.StatusConflict, http.StatusPreconditionFailed:
		return sdkgo.FailureConflict
	case http.StatusTooManyRequests:
		return sdkgo.FailureRateLimit
	default:
		return sdkgo.FailureProviderRejection
	}
}

func isSuccessStatus(statusCode int) bool { return statusCode >= 200 && statusCode < 300 }

// isConclusiveRejectionStatus reports a 4xx other than 408 and 429, or a 501: Graph refused before storing anything.
func isConclusiveRejectionStatus(statusCode int) bool {
	if statusCode == http.StatusNotImplemented {
		return true
	}
	return statusCode >= 400 && statusCode < 500 && statusCode != http.StatusRequestTimeout && statusCode != http.StatusTooManyRequests
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
