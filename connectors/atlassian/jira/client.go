// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package jira searches, reads, creates, transitions, and comments on Jira
// Cloud issues as Dex connector Steps, through Atlassian's OAuth 2.0 (3LO)
// gateway at https://api.atlassian.com/ex/jira/{cloudId}/rest/api/3.
//
// Jira accepts no idempotency key, so CreateIssue and AddComment never resend
// a request Jira may have received: an ambiguous outcome selects the uncertain
// branch. TransitionIssue reads the issue before it writes and after an
// ambiguous write, and Jira refuses a transition that is not available from the
// current status, so a repeated transition cannot move an issue twice.
package jira

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
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "jira"

	// operationDeadline keeps every request of one Invoke inside the 30-second Execute timeout.
	operationDeadline = 25 * time.Second
	// requestTimeout bounds one exchange, so a hung write selects uncertain before Dex times the Step out.
	requestTimeout = 20 * time.Second

	jiraRESTPathPrefix      = "/rest/api/3"
	accessibleResourcesPath = "/oauth/token/accessible-resources"
	requestIDHeader         = "X-Arequestid"
	traceIDHeader           = "Atl-Traceid"
	maximumRejectedFieldIDs = 20
	maximumSafeTokenBytes   = 128
)

var (
	cloudIDPattern       = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	jiraFieldIDPattern   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)
	jiraSiteScopeMarkers = []string{"read:jira-work", "write:jira-work"}
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient replaces the HTTP client used for Jira and Atlassian token
// requests. The caller keeps ownership of client and its transport. Jira
// requests use a copy that never follows redirects and is bounded by 20
// seconds, so a hung write selects uncertain before Dex's 30-second Execute
// timeout.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Jira Cloud REST requests for one connection.
// It is safe for concurrent use by several Steps.
type Client struct {
	endpoint          string
	configuredCloudID string
	httpClient        *http.Client
	credentials       sdkgo.CredentialProvider[Credentials]
	refreshDriver     sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes  int64
	now               func() time.Time

	siteMutex       sync.Mutex
	resolvedCloudID string
}

// jiraRequest is one Jira REST call below /ex/jira/{cloudId}/rest/api/3, or one gateway call.
type jiraRequest struct {
	method        string
	path          string
	query         url.Values
	payload       any
	isGatewayPath bool
}

// jiraResponse is one bounded Jira response. The body is empty when it was oversized or unreadable.
type jiraResponse struct {
	statusCode        int
	header            http.Header
	body              []byte
	isBodyTooLarge    bool
	hasBodyReadFailed bool
	reflectsSecret    bool
}

// exchangeResult reports one authenticated exchange; isDispatched false proves Jira received nothing.
type exchangeResult struct {
	response     jiraResponse
	isDispatched bool
	err          error
}

// operationSession carries the resolved credential and Jira site for one Invoke.
type operationSession struct {
	context     context.Context
	call        sdkgo.Call
	credentials Credentials
	cloudID     string
}

// sessionFailure explains why an operation could not start; isRetryable selects Retry instead of defect.
type sessionFailure struct {
	failure     sdkgo.Failure
	isRetryable bool
	retryAfter  time.Duration
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
	writeDefect
)

// writeClassification is the safe meaning of one write exchange.
type writeClassification struct {
	outcome          writeOutcome
	failure          sdkgo.Failure
	retryAfter       time.Duration
	rejectedFieldIDs []string
}

// dispatchObservation records whether a request could have reached Jira.
type dispatchObservation struct {
	mutex                 sync.Mutex
	hasObtainedConnection bool
	hasFailedToConnect    bool
}

type accessibleResource struct {
	ID     string   `json:"id"`
	Scopes []string `json:"scopes"`
}

type jiraErrorCollection struct {
	Errors map[string]json.RawMessage `json:"errors"`
}

// New validates configuration and constructs an authenticated Jira Cloud client.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Jira endpoint: %w", err)
	}
	cloudID := strings.TrimSpace(config.CloudID)
	if cloudID != "" && !cloudIDPattern.MatchString(cloudID) {
		return nil, fmt.Errorf("Jira cloudId must be a site UUID such as 1324a887-45db-1bf4-1e99-ef0ff456d421")
	}
	if config.MaxResponseBytes < 1 {
		return nil, fmt.Errorf("Jira maxResponseBytes must be positive")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Jira connector option is nil")
		}
		option(&dependencies)
	}
	return &Client{
		endpoint: endpoint, configuredCloudID: strings.ToLower(cloudID),
		httpClient:       providerhttp.NewProviderHTTPClient(dependencies.httpClient, requestTimeout),
		credentials:      credentials,
		refreshDriver:    NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes: config.MaxResponseBytes, now: time.Now,
	}, nil
}

// SearchIssues returns the searchIssues Query bound to this client.
func (client *Client) SearchIssues() SearchIssuesOperation {
	return SearchIssuesOperation{client: client}
}

// GetIssue returns the getIssue Query bound to this client.
func (client *Client) GetIssue() GetIssueOperation { return GetIssueOperation{client: client} }

// CreateIssue returns the createIssue Mutation bound to this client.
func (client *Client) CreateIssue() CreateIssueOperation { return CreateIssueOperation{client: client} }

// TransitionIssue returns the transitionIssue Mutation bound to this client.
func (client *Client) TransitionIssue() TransitionIssueOperation {
	return TransitionIssueOperation{client: client}
}

// AddComment returns the addComment Mutation bound to this client.
func (client *Client) AddComment() AddCommentOperation { return AddCommentOperation{client: client} }

// startSession resolves the credential and the Jira site; the caller must call the returned cancel.
func (client *Client) startSession(call sdkgo.Call, operationID string) (*operationSession, context.CancelFunc, *sessionFailure) {
	operationContext, cancel := context.WithTimeout(call.Context, operationDeadline)
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		cancel()
		return nil, nil, &sessionFailure{failure: newFailure(operationID, sdkgo.FailureAuthentication, "Jira connection credentials are unavailable; reconnect the connection")}
	}
	session := &operationSession{context: operationContext, call: call, credentials: credentials}
	cloudID, failure := client.siteCloudID(session, operationID)
	if failure != nil {
		cancel()
		return nil, nil, failure
	}
	session.cloudID = cloudID
	return session, cancel, nil
}

// siteCloudID returns the configured site, or the only Jira site the authorization grants.
func (client *Client) siteCloudID(session *operationSession, operationID string) (string, *sessionFailure) {
	if client.configuredCloudID != "" {
		return client.configuredCloudID, nil
	}
	client.siteMutex.Lock()
	defer client.siteMutex.Unlock()
	if client.resolvedCloudID != "" {
		return client.resolvedCloudID, nil
	}
	result := client.exchange(session, jiraRequest{method: http.MethodGet, path: accessibleResourcesPath, isGatewayPath: true})
	if result.err != nil {
		return "", &sessionFailure{failure: newFailure(operationID, sdkgo.FailureTransport, "Atlassian accessible resources are temporarily unavailable"), isRetryable: true}
	}
	response := result.response
	switch {
	case response.statusCode == http.StatusTooManyRequests:
		return "", &sessionFailure{
			failure:     newFailure(operationID, sdkgo.FailureRateLimit, "Atlassian rate limited the accessible resources lookup"),
			isRetryable: true, retryAfter: client.retryAfter(response),
		}
	case response.statusCode == http.StatusRequestTimeout || response.statusCode >= 500 || response.hasBodyReadFailed:
		return "", &sessionFailure{failure: newFailure(operationID, sdkgo.FailureAvailability, "Atlassian accessible resources are temporarily unavailable"), isRetryable: true}
	case response.statusCode < 200 || response.statusCode >= 300:
		return "", &sessionFailure{failure: newFailure(operationID, statusFailureKind(response.statusCode),
			fmt.Sprintf("Atlassian rejected the accessible resources lookup with HTTP %d; reconnect the connection or set cloudId", response.statusCode))}
	case response.isBodyTooLarge || response.reflectsSecret:
		return "", &sessionFailure{failure: newFailure(operationID, sdkgo.FailureProtocol, "Atlassian returned an unusable accessible resources list; set cloudId")}
	}
	var resources []accessibleResource
	if err := json.Unmarshal(response.body, &resources); err != nil {
		return "", &sessionFailure{failure: newFailure(operationID, sdkgo.FailureProtocol, "Atlassian returned an invalid accessible resources list; set cloudId")}
	}
	siteIDs := jiraSiteCloudIDs(resources)
	switch len(siteIDs) {
	case 0:
		return "", &sessionFailure{failure: newFailure(operationID, sdkgo.FailureAuthorization, "the authorization grants no Jira site; reconnect and pick a Jira site on the consent screen")}
	case 1:
		client.resolvedCloudID = siteIDs[0]
		return client.resolvedCloudID, nil
	default:
		return "", &sessionFailure{failure: newFailure(operationID, sdkgo.FailureValidation,
			fmt.Sprintf("the authorization grants %d Jira sites; choose one with the Jira site picker or set cloudId", len(siteIDs)))}
	}
}

// jiraSiteCloudIDs returns the distinct, valid cloud IDs of resources that carry a Jira scope.
func jiraSiteCloudIDs(resources []accessibleResource) []string {
	seen := map[string]bool{}
	var siteIDs []string
	for _, resource := range resources {
		cloudID := strings.ToLower(resource.ID)
		if !cloudIDPattern.MatchString(cloudID) || seen[cloudID] || !hasAnyScope(resource.Scopes, jiraSiteScopeMarkers) {
			continue
		}
		seen[cloudID] = true
		siteIDs = append(siteIDs, cloudID)
	}
	sort.Strings(siteIDs)
	return siteIDs
}

// exchange refreshes and resends once after a 401; Jira rejects unauthenticated writes before acting.
func (client *Client) exchange(session *operationSession, request jiraRequest) exchangeResult {
	var encodedPayload []byte
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return exchangeResult{err: errRequestNotBuilt}
		}
		encodedPayload = encoded
	}
	target := client.requestURL(session, request)
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

func (client *Client) requestURL(session *operationSession, request jiraRequest) string {
	target := client.endpoint
	if !request.isGatewayPath {
		target += "/ex/jira/" + url.PathEscape(session.cloudID) + jiraRESTPathPrefix
	}
	target += request.path
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
	response := jiraResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
	limit := client.maxResponseBytes
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
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
		return readClassification{outcome: readDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, "Jira request could not be built")}
	}
	if result.err != nil {
		return readClassification{outcome: readRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Jira "+subject+" is temporarily unavailable")}
	}
	response := result.response
	switch {
	case response.statusCode == http.StatusNotFound:
		return readClassification{outcome: readNotFound, failure: newFailure(operationID, sdkgo.FailureNotFound, "Jira "+subject+" was not found or is not visible to the connection")}
	case response.statusCode == http.StatusTooManyRequests:
		return readClassification{outcome: readRetry, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureRateLimit, "Jira rate limited the "+subject)}
	case isRetryableReadStatus(response.statusCode):
		return readClassification{outcome: readRetry, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureAvailability, "Jira "+subject+" is temporarily unavailable")}
	case response.statusCode >= 400 && response.statusCode < 500:
		return readClassification{outcome: readRejected, failure: rejectionFailure(operationID, subject, response.statusCode, readJiraRejectedFieldIDs(response.body))}
	case !isSuccessStatus(response.statusCode):
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, fmt.Sprintf("Jira returned unexpected HTTP %d for the %s", response.statusCode, subject))}
	case response.hasBodyReadFailed:
		return readClassification{outcome: readRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Jira "+subject+" response was interrupted")}
	case response.isBodyTooLarge:
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureResponseTooLarge, "Jira "+subject+" response exceeds the configured size limit")}
	case response.reflectsSecret:
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Jira "+subject+" response contains credential material")}
	default:
		return readClassification{outcome: readSucceeded}
	}
}

// classifyUnkeyedWrite retries only a 429 or an undispatched request, when Jira cannot have written.
func (client *Client) classifyUnkeyedWrite(operationID string, subject string, result exchangeResult) writeClassification {
	switch {
	case errors.Is(result.err, errRequestNotBuilt):
		return writeClassification{outcome: writeDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, "Jira request could not be built")}
	case result.err != nil && !result.isDispatched:
		return writeClassification{outcome: writeRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Jira could not be reached, so no "+subject+" was written")}
	case result.err != nil:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureTransport, "Jira "+subject+" outcome is unknown")}
	}
	response := result.response
	switch {
	case isSuccessStatus(response.statusCode) && response.hasBodyReadFailed:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureTransport, "Jira accepted the "+subject+" but its response was interrupted")}
	case isSuccessStatus(response.statusCode) && response.isBodyTooLarge:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureResponseTooLarge, "Jira accepted the "+subject+" but its response exceeds the configured size limit")}
	case isSuccessStatus(response.statusCode) && response.reflectsSecret:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureProtocol, "Jira accepted the "+subject+" but its response contains credential material")}
	case isSuccessStatus(response.statusCode):
		return writeClassification{outcome: writeAccepted}
	case response.statusCode == http.StatusTooManyRequests:
		return writeClassification{outcome: writeRetry, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureRateLimit, "Jira rate limited the "+subject+" before writing it")}
	case response.statusCode == http.StatusNotFound:
		return writeClassification{outcome: writeNotFound, failure: rejectionFailure(operationID, subject, response.statusCode, nil)}
	case isConclusiveRejectionStatus(response.statusCode):
		rejectedFieldIDs := readJiraRejectedFieldIDs(response.body)
		return writeClassification{outcome: writeRejected, rejectedFieldIDs: rejectedFieldIDs, failure: rejectionFailure(operationID, subject, response.statusCode, rejectedFieldIDs)}
	default:
		return writeClassification{outcome: writeUncertain, failure: newFailure(operationID, sdkgo.FailureAvailability, fmt.Sprintf("Jira %s outcome is unknown after HTTP %d", subject, response.statusCode))}
	}
}

func (client *Client) retryAfter(response jiraResponse) time.Duration {
	return providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
}

func (client *Client) receipt(session *operationSession, response jiraResponse, objectID string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: session.call.ID, IdempotencyKey: session.call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
	for _, header := range []string{requestIDHeader, traceIDHeader} {
		if value := response.header.Get(header); isSafeProviderToken(value) {
			receipt.ProviderRequestID = value
			break
		}
	}
	return receipt
}

func (client *Client) emptyReceipt(session *operationSession) sdkgo.Receipt {
	return client.receipt(session, jiraResponse{header: http.Header{}}, "")
}

// readJiraRejectedFieldIDs returns the sorted, safe field IDs of a Jira errors map; messages are never read.
func readJiraRejectedFieldIDs(body []byte) []string {
	var collection jiraErrorCollection
	if len(body) == 0 || json.Unmarshal(body, &collection) != nil {
		return nil
	}
	var fieldIDs []string
	for fieldID := range collection.Errors {
		if jiraFieldIDPattern.MatchString(fieldID) {
			fieldIDs = append(fieldIDs, fieldID)
		}
	}
	sort.Strings(fieldIDs)
	if len(fieldIDs) > maximumRejectedFieldIDs {
		fieldIDs = fieldIDs[:maximumRejectedFieldIDs]
	}
	return fieldIDs
}

// rejectionFailure describes a conclusive Jira rejection by status and field IDs, never by provider text.
func rejectionFailure(operationID string, subject string, statusCode int, rejectedFieldIDs []string) sdkgo.Failure {
	message := fmt.Sprintf("Jira rejected the %s with HTTP %d", subject, statusCode)
	if len(rejectedFieldIDs) > 0 {
		message += " (fields: " + strings.Join(rejectedFieldIDs, ", ") + ")"
	}
	return newFailure(operationID, statusFailureKind(statusCode), message)
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

// isConclusiveRejectionStatus reports a 4xx other than 408 and 429: Jira refused before changing anything.
func isConclusiveRejectionStatus(statusCode int) bool {
	return statusCode >= 400 && statusCode < 500 && statusCode != http.StatusRequestTimeout && statusCode != http.StatusTooManyRequests
}

func isRetryableReadStatus(statusCode int) bool {
	return statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooManyRequests || statusCode >= 500
}

func hasAnyScope(scopes []string, wanted []string) bool {
	for _, scope := range scopes {
		for _, candidate := range wanted {
			if scope == candidate {
				return true
			}
		}
	}
	return false
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

func (failure *sessionFailure) pointer() *sdkgo.Failure { return &failure.failure }

var errRequestNotBuilt = errors.New("Jira request could not be built")

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
