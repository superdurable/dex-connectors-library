// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package confluence searches, reads, publishes, updates, and comments on
// Confluence Cloud pages as Dex connector Steps, through Atlassian's OAuth 2.0
// (3LO) gateway at https://api.atlassian.com/ex/confluence/{cloudId}/wiki.
//
// Confluence accepts no idempotency key, so every write relies on a provider
// rule instead. Confluence allows one page per title in a space, so CreatePage
// recognizes a page an earlier attempt created. A page update names the next
// version number, so UpdatePage recognizes its own version by a marker in the
// version message. AddComment sends a comment at most once per Step execution
// and confirms an unconfirmed attempt by reading the page's comments.
package confluence

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
	providerName = "confluence"

	// operationDeadline keeps every request of one Invoke inside the 30-second Execute timeout.
	operationDeadline = 25 * time.Second
	// requestTimeout bounds one exchange, so a hung write is reconciled before Dex times the Step out.
	requestTimeout = 20 * time.Second

	confluencePathPrefix    = "/ex/confluence/"
	contentAPIPathPrefix    = "/wiki/api/v2"
	searchAPIPathPrefix     = "/wiki/rest/api"
	accessibleResourcesPath = "/oauth/token/accessible-resources"
	traceIDHeader           = "Atl-Traceid"
	maximumSafeTokenBytes   = 128
	maximumWebURLBytes      = 2048
)

var (
	cloudIDPattern     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	contentIDPattern   = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	accountIDPattern   = regexp.MustCompile(`^[A-Za-z0-9:_-]{1,128}$`)
	webUIPathPattern   = regexp.MustCompile(`^/[\x21-\x7e]*$`)
	errRequestNotBuilt = errors.New("Confluence request could not be built")
)

// confluenceAPI selects the base path of one request.
type confluenceAPI uint8

const (
	// contentAPI is the REST API v2 below /ex/confluence/{cloudId}/wiki/api/v2.
	contentAPI confluenceAPI = iota + 1
	// searchAPI is the REST API v1 CQL search below /ex/confluence/{cloudId}/wiki/rest/api.
	searchAPI
	// gatewayAPI is a path on the Atlassian gateway itself, such as accessible-resources.
	gatewayAPI
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient replaces the HTTP client used for Confluence and Atlassian
// token requests. The caller keeps ownership of client and its transport.
// Confluence requests use a copy that never follows redirects and is bounded
// by 20 seconds, so a hung write is reconciled before Dex's 30-second Execute
// timeout.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Confluence Cloud REST requests for one
// connection. It is safe for concurrent use by several Steps.
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

// confluenceRequest is one Confluence REST call or one gateway call.
type confluenceRequest struct {
	method  string
	api     confluenceAPI
	path    string
	query   url.Values
	payload any
}

// confluenceResponse is one bounded response. The body is empty when it was oversized or unreadable.
type confluenceResponse struct {
	statusCode        int
	header            http.Header
	body              []byte
	isBodyTooLarge    bool
	hasBodyReadFailed bool
	reflectsSecret    bool
}

// exchangeResult reports one authenticated exchange; isDispatched false proves Confluence received nothing.
type exchangeResult struct {
	response     confluenceResponse
	isDispatched bool
	err          error
}

// operationSession carries the resolved credential and Confluence site for one Invoke.
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
	// writeAccepted is a 2xx whose body was read in full.
	writeAccepted writeOutcome = iota + 1
	// writeNotApplied is a 429 or an undispatched request: Confluence cannot have written.
	writeNotApplied
	writeNotFound
	writeRejected
	// writeAmbiguous may have been applied: a lost response, 3xx, 408, 5xx, or an unusable 2xx body.
	writeAmbiguous
	writeDefect
)

// writeClassification is the safe meaning of one write exchange.
type writeClassification struct {
	outcome    writeOutcome
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// dispatchObservation records whether a request could have reached Confluence.
type dispatchObservation struct {
	mutex                 sync.Mutex
	hasObtainedConnection bool
	hasFailedToConnect    bool
}

type accessibleResource struct {
	ID     string   `json:"id"`
	Scopes []string `json:"scopes"`
}

// New validates configuration and constructs an authenticated Confluence Cloud client.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Confluence endpoint: %w", err)
	}
	cloudID := strings.TrimSpace(config.CloudID)
	if cloudID != "" && !cloudIDPattern.MatchString(cloudID) {
		return nil, fmt.Errorf("Confluence cloudId must be a site UUID such as 1324a887-45db-1bf4-1e99-ef0ff456d421")
	}
	if config.MaxResponseBytes < 1 {
		return nil, fmt.Errorf("Confluence maxResponseBytes must be positive")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Confluence connector option is nil")
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

// SearchPages returns the searchPages Query bound to this client.
func (client *Client) SearchPages() SearchPagesOperation { return SearchPagesOperation{client: client} }

// GetPage returns the getPage Query bound to this client.
func (client *Client) GetPage() GetPageOperation { return GetPageOperation{client: client} }

// CreatePage returns the createPage Mutation bound to this client.
func (client *Client) CreatePage() CreatePageOperation { return CreatePageOperation{client: client} }

// UpdatePage returns the updatePage Mutation bound to this client.
func (client *Client) UpdatePage() UpdatePageOperation { return UpdatePageOperation{client: client} }

// AddComment returns the addComment Mutation bound to this client.
func (client *Client) AddComment() AddCommentOperation { return AddCommentOperation{client: client} }

// startSession resolves the credential and the Confluence site; the caller must call the returned cancel.
func (client *Client) startSession(call sdkgo.Call, operationID string) (*operationSession, context.CancelFunc, *sessionFailure) {
	operationContext, cancel := context.WithTimeout(call.Context, operationDeadline)
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		cancel()
		return nil, nil, &sessionFailure{failure: newFailure(operationID, sdkgo.FailureAuthentication, "Confluence connection credentials are unavailable; reconnect the connection")}
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

// siteCloudID returns the configured site, or the only Confluence site the authorization grants.
func (client *Client) siteCloudID(session *operationSession, operationID string) (string, *sessionFailure) {
	if client.configuredCloudID != "" {
		return client.configuredCloudID, nil
	}
	client.siteMutex.Lock()
	defer client.siteMutex.Unlock()
	if client.resolvedCloudID != "" {
		return client.resolvedCloudID, nil
	}
	result := client.exchange(session, confluenceRequest{method: http.MethodGet, api: gatewayAPI, path: accessibleResourcesPath})
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
	case !isSuccessStatus(response.statusCode):
		return "", &sessionFailure{failure: newFailure(operationID, statusFailureKind(response.statusCode),
			fmt.Sprintf("Atlassian rejected the accessible resources lookup with HTTP %d; reconnect the connection or set cloudId", response.statusCode))}
	case response.isBodyTooLarge || response.reflectsSecret:
		return "", &sessionFailure{failure: newFailure(operationID, sdkgo.FailureProtocol, "Atlassian returned an unusable accessible resources list; set cloudId")}
	}
	var resources []accessibleResource
	if err := json.Unmarshal(response.body, &resources); err != nil {
		return "", &sessionFailure{failure: newFailure(operationID, sdkgo.FailureProtocol, "Atlassian returned an invalid accessible resources list; set cloudId")}
	}
	siteIDs := confluenceSiteCloudIDs(resources)
	switch len(siteIDs) {
	case 0:
		return "", &sessionFailure{failure: newFailure(operationID, sdkgo.FailureAuthorization, "the authorization grants no Confluence site; reconnect and pick a Confluence site on the consent screen")}
	case 1:
		client.resolvedCloudID = siteIDs[0]
		return client.resolvedCloudID, nil
	default:
		return "", &sessionFailure{failure: newFailure(operationID, sdkgo.FailureValidation,
			fmt.Sprintf("the authorization grants %d Confluence sites; choose one with the Confluence site picker or set cloudId", len(siteIDs)))}
	}
}

// confluenceSiteCloudIDs returns the distinct, valid cloud IDs of resources that carry a Confluence scope.
func confluenceSiteCloudIDs(resources []accessibleResource) []string {
	seen := map[string]bool{}
	var siteIDs []string
	for _, resource := range resources {
		cloudID := strings.ToLower(resource.ID)
		if !cloudIDPattern.MatchString(cloudID) || seen[cloudID] || !hasConfluenceScope(resource.Scopes) {
			continue
		}
		seen[cloudID] = true
		siteIDs = append(siteIDs, cloudID)
	}
	sort.Strings(siteIDs)
	return siteIDs
}

// exchange refreshes and resends once after a 401; Confluence rejects unauthenticated writes before acting.
func (client *Client) exchange(session *operationSession, request confluenceRequest) exchangeResult {
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

func (client *Client) requestURL(session *operationSession, request confluenceRequest) string {
	target := client.endpoint
	switch request.api {
	case contentAPI:
		target += confluencePathPrefix + url.PathEscape(session.cloudID) + contentAPIPathPrefix
	case searchAPI:
		target += confluencePathPrefix + url.PathEscape(session.cloudID) + searchAPIPathPrefix
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
	response := confluenceResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
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
		return readClassification{outcome: readDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, "Confluence request could not be built")}
	}
	if result.err != nil {
		return readClassification{outcome: readRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Confluence "+subject+" is temporarily unavailable")}
	}
	response := result.response
	switch {
	case response.statusCode == http.StatusNotFound:
		return readClassification{outcome: readNotFound, failure: newFailure(operationID, sdkgo.FailureNotFound, "Confluence "+subject+" was not found or is not visible to the connection")}
	case response.statusCode == http.StatusTooManyRequests:
		return readClassification{outcome: readRetry, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureRateLimit, "Confluence rate limited the "+subject)}
	case response.statusCode == http.StatusRequestTimeout || response.statusCode >= 500:
		return readClassification{outcome: readRetry, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureAvailability, "Confluence "+subject+" is temporarily unavailable")}
	case response.statusCode >= 400 && response.statusCode < 500:
		return readClassification{outcome: readRejected, failure: rejectionFailure(operationID, subject, response.statusCode)}
	case !isSuccessStatus(response.statusCode):
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, fmt.Sprintf("Confluence returned unexpected HTTP %d for the %s", response.statusCode, subject))}
	case response.hasBodyReadFailed:
		return readClassification{outcome: readRetry, failure: newFailure(operationID, sdkgo.FailureTransport, "Confluence "+subject+" response was interrupted")}
	case response.isBodyTooLarge:
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureResponseTooLarge, "Confluence "+subject+" response exceeds the configured size limit")}
	case response.reflectsSecret:
		return readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Confluence "+subject+" response contains credential material")}
	default:
		return readClassification{outcome: readSucceeded}
	}
}

// classifyWrite separates writes Confluence cannot have applied from conclusive and ambiguous outcomes.
func (client *Client) classifyWrite(operationID string, subject string, result exchangeResult) writeClassification {
	switch {
	case errors.Is(result.err, errRequestNotBuilt):
		return writeClassification{outcome: writeDefect, failure: newFailure(operationID, sdkgo.FailureLocalDefect, "Confluence request could not be built")}
	case result.err != nil && !result.isDispatched:
		return writeClassification{outcome: writeNotApplied, failure: newFailure(operationID, sdkgo.FailureTransport, "Confluence could not be reached, so no "+subject+" was written")}
	case result.err != nil:
		return writeClassification{outcome: writeAmbiguous, failure: newFailure(operationID, sdkgo.FailureTransport, "Confluence "+subject+" outcome is unknown")}
	}
	response := result.response
	switch {
	case isSuccessStatus(response.statusCode) && response.hasBodyReadFailed:
		return writeClassification{outcome: writeAmbiguous, failure: newFailure(operationID, sdkgo.FailureTransport, "Confluence accepted the "+subject+" but its response was interrupted")}
	case isSuccessStatus(response.statusCode) && response.isBodyTooLarge:
		return writeClassification{outcome: writeAmbiguous, failure: newFailure(operationID, sdkgo.FailureResponseTooLarge, "Confluence accepted the "+subject+" but its response exceeds the configured size limit")}
	case isSuccessStatus(response.statusCode) && response.reflectsSecret:
		return writeClassification{outcome: writeAmbiguous, failure: newFailure(operationID, sdkgo.FailureProtocol, "Confluence accepted the "+subject+" but its response contains credential material")}
	case isSuccessStatus(response.statusCode):
		return writeClassification{outcome: writeAccepted}
	case response.statusCode == http.StatusTooManyRequests:
		return writeClassification{outcome: writeNotApplied, retryAfter: client.retryAfter(response), failure: newFailure(operationID, sdkgo.FailureRateLimit, "Confluence rate limited the "+subject+" before writing it")}
	case response.statusCode == http.StatusNotFound:
		return writeClassification{outcome: writeNotFound, failure: rejectionFailure(operationID, subject, response.statusCode)}
	case isConclusiveRejectionStatus(response.statusCode):
		return writeClassification{outcome: writeRejected, failure: rejectionFailure(operationID, subject, response.statusCode)}
	default:
		return writeClassification{outcome: writeAmbiguous, retryAfter: client.retryAfter(response),
			failure: newFailure(operationID, sdkgo.FailureAvailability, fmt.Sprintf("Confluence %s outcome is unknown after HTTP %d", subject, response.statusCode))}
	}
}

func (client *Client) retryAfter(response confluenceResponse) time.Duration {
	return providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
}

func (client *Client) receipt(session *operationSession, response confluenceResponse, objectID string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: session.call.ID, IdempotencyKey: session.call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
	if response.header != nil {
		if value := response.header.Get(traceIDHeader); isSafeProviderToken(value) {
			receipt.ProviderRequestID = value
		}
	}
	return receipt
}

// rejectionFailure describes a conclusive rejection by HTTP status, never by provider text.
func rejectionFailure(operationID string, subject string, statusCode int) sdkgo.Failure {
	return newFailure(operationID, statusFailureKind(statusCode), fmt.Sprintf("Confluence rejected the %s with HTTP %d", subject, statusCode))
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

// isConclusiveRejectionStatus reports a 4xx other than 408 and 429: Confluence refused before changing anything.
func isConclusiveRejectionStatus(statusCode int) bool {
	return statusCode >= 400 && statusCode < 500 && statusCode != http.StatusRequestTimeout && statusCode != http.StatusTooManyRequests
}

// hasConfluenceScope recognizes classic and granular Confluence scopes, such as read:page:confluence.
func hasConfluenceScope(scopes []string) bool {
	for _, scope := range scopes {
		if strings.Contains(scope, "confluence") {
			return true
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

// buildWebURL joins Confluence's site base and web UI path, or returns "" when either is unusable.
func buildWebURL(base string, webUIPath string) string {
	if !strings.HasPrefix(base, "https://") || !webUIPathPattern.MatchString(webUIPath) {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	webURL := strings.TrimRight(base, "/") + webUIPath
	if len(webURL) > maximumWebURLBytes {
		return ""
	}
	return webURL
}

func newFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operationID, Message: message}
}

func failurePointer(operationID string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := newFailure(operationID, kind, message)
	return &failure
}

func (failure *sessionFailure) pointer() *sdkgo.Failure { return &failure.failure }

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
