// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package outlookmail connects Dex applications to Microsoft 365 (Exchange Online) mailboxes through
// Microsoft Graph v1.0 at https://graph.microsoft.com/v1.0.
//
// A Client exposes six operations. SearchMessages and GetMessage read, SendMessage and ReplyToMessage
// send, and MoveMessage and SetMessageFlags organize. Every request asks Graph for immutable message
// IDs, so an ID from searchMessages keeps naming the same message after moveMessage.
//
// Graph has no idempotency key for mail. SendMessage and ReplyToMessage therefore create a draft that
// carries a marker derived from the Step's idempotency key, record a Dex heartbeat checkpoint, and send
// that draft. A retry finds its own draft by the marker instead of creating another, never sends a
// dispatched draft again, and confirms a lost answer by reading the draft's Sent Items copy. Both run
// with sync durability, so Dex never runs two attempts at once.
//
// A connection authenticates with delegated Microsoft OAuth, whose rotating refresh token
// CredentialRefreshDriver exchanges, or app-only with a tenant's client credentials for one configured
// mailbox.
package outlookmail

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// MicrosoftOAuthAuthMethodID identifies a connection a Microsoft 365 user authorized through OAuth consent.
	MicrosoftOAuthAuthMethodID = "microsoft-oauth"
	// AppOnlyAuthMethodID identifies a connection whose app uses client credentials for one configured mailbox.
	AppOnlyAuthMethodID = "app-only"

	providerName    = "microsoft-graph"
	graphHost       = "graph.microsoft.com"
	loginHost       = "login.microsoftonline.com"
	graphAPIVersion = "/v1.0"
	graphBaseURL    = "https://" + graphHost + graphAPIVersion
	delegatedRoot   = "/me"

	// defaultRequestTimeout bounds one Graph request inside the 30-second and 90-second Execute timeouts.
	defaultRequestTimeout = 20 * time.Second
	// maximumNextPageLinkBytes bounds a next-page cursor carried in Flow state.
	maximumNextPageLinkBytes = 4096

	requestIDHeader       = "request-id"
	preferHeader          = "Prefer"
	immutableIDPreference = `IdType="ImmutableId"`
	textBodyPreference    = `outlook.body-content-type="text"`
)

var (
	errRequestNotBuilt = errors.New("Microsoft Graph request could not be built")
	requestIDPattern   = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	guidPattern        = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient       *http.Client
	localProviderURL string
	now              func() time.Time
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 20 seconds per request. The caller
// retains ownership of the client and its transport. The connector uses a copy that never follows
// redirects, so a token is never replayed to another host. The same client carries the token requests
// to https://login.microsoftonline.com.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithLocalProviderURL sends every request for https://graph.microsoft.com and
// https://login.microsoftonline.com to one local Graph-compatible fake, keeping each request's path,
// for local verification only. The URL must be an http or https URL whose host is loopback, without a
// path, user information, a query, or a fragment, and the transport refuses every other host.
// Production connections leave it unset.
func WithLocalProviderURL(baseURL string) Option {
	return func(options *clientOptions) { options.localProviderURL = baseURL }
}

// WithClock overrides the clock used for Receipts, Retry-After dates, and token expiry. Tests use it;
// production connections leave it unset.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// Client executes authenticated Microsoft Graph mail requests for connector operations. A Client is safe
// for concurrent use by several Steps.
type Client struct {
	httpClient       *http.Client
	credentials      CredentialSource
	refreshDriver    *CredentialRefreshDriver
	appOnlyMailbox   string
	maxResponseBytes int64
	now              func() time.Time
}

// New validates configuration and constructs a client. Credentials are resolved before every operation,
// so a replaced secret or a refreshed token takes effect without a restart; the app-only mailbox and the
// response limit are startup configuration. Construction never contacts Microsoft.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Microsoft Graph response limit must be positive")
	}
	mailbox := strings.TrimSpace(config.Mailbox)
	if mailbox != "" {
		if err := validateMailbox(mailbox); err != nil {
			return nil, err
		}
	}
	if credentials == nil {
		return nil, errors.New("Outlook Mail credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Outlook Mail connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("Outlook Mail connector clock is required")
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
	refreshDriver := NewCredentialRefreshDriver(httpClient)
	refreshDriver.now = dependencies.now
	return &Client{
		httpClient: httpClient, credentials: credentials, refreshDriver: refreshDriver, appOnlyMailbox: mailbox,
		maxResponseBytes: config.MaxResponseBytes, now: dependencies.now,
	}, nil
}

// SearchMessages returns the searchMessages Query bound to this client.
func (client *Client) SearchMessages() SearchMessagesOperation {
	return SearchMessagesOperation{client: client}
}

// GetMessage returns the getMessage Query bound to this client.
func (client *Client) GetMessage() GetMessageOperation { return GetMessageOperation{client: client} }

// SendMessage returns the sendMessage Mutation bound to this client.
func (client *Client) SendMessage() SendMessageOperation { return SendMessageOperation{client: client} }

// ReplyToMessage returns the replyToMessage Mutation bound to this client.
func (client *Client) ReplyToMessage() ReplyToMessageOperation {
	return ReplyToMessageOperation{client: client}
}

// MoveMessage returns the moveMessage Mutation bound to this client.
func (client *Client) MoveMessage() MoveMessageOperation { return MoveMessageOperation{client: client} }

// SetMessageFlags returns the setMessageFlags Mutation bound to this client.
func (client *Client) SetMessageFlags() SetMessageFlagsOperation {
	return SetMessageFlagsOperation{client: client}
}

// graphSession is one operation's resolved credentials and mailbox path; it refreshes after a 401 once.
type graphSession struct {
	client                     *Client
	call                       sdkgo.Call
	operation                  string
	credentials                Credentials
	mailboxRoot                string
	hasRefreshedAfterRejection bool
}

// graphRequest is one Graph request below the session's mailbox, or a validated next-page link.
type graphRequest struct {
	method          string
	path            string
	absoluteURL     string
	query           graphQuery
	payload         any
	prefersTextBody bool
}

// graphQuery is an ordered list of query parameters, encoded with %20 for spaces as OData expects.
type graphQuery []graphQueryParameter

type graphQueryParameter struct {
	name  string
	value string
}

// graphOutcome is the provider-neutral meaning of one Graph exchange that each operation maps to a branch.
type graphOutcome uint8

const (
	graphSucceeded graphOutcome = iota + 1
	// graphNotFound is a 404 or malformed-ID answer about the addressed message or folder.
	graphNotFound
	// graphRetry is an answer Graph sends before doing anything, such as 429, so any request may repeat.
	graphRetry
	// graphRejected is a conclusive refusal.
	graphRejected
	// graphInvalid is an unusable 2xx: oversized, malformed, or reflecting the credential.
	graphInvalid
	// graphAmbiguous is a server error or lost answer after which a write may have happened.
	graphAmbiguous
	graphDefect
)

// graphExchange is one classified exchange; failure is set for every outcome except graphSucceeded.
type graphExchange struct {
	outcome    graphOutcome
	statusCode int
	body       []byte
	requestID  string
	errorCode  string
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// openSession resolves credentials and the mailbox path, separating a revoked grant from a transient failure.
func (client *Client) openSession(call sdkgo.Call, operation string) (*graphSession, *graphExchange) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return nil, &graphExchange{outcome: graphRejected, failure: graphFailure(sdkgo.FailureAuthentication, operation,
			"Microsoft authorization must be renewed: authorize the connection again or replace the client secret")}
	case errors.Is(err, errCredentialRefreshUnavailable):
		return nil, &graphExchange{outcome: graphRetry, failure: graphFailure(sdkgo.FailureAvailability, operation,
			"the Microsoft token endpoint is temporarily unavailable")}
	case err != nil || validateResolvedCredentials(credentials) != nil:
		return nil, &graphExchange{outcome: graphDefect, failure: graphFailure(sdkgo.FailureAuthentication, operation,
			"Outlook Mail connection credentials are unavailable")}
	}
	mailboxRoot, err := client.resolveMailboxRoot(credentials.AuthMethodID)
	if err != nil {
		return nil, &graphExchange{outcome: graphDefect, failure: graphFailure(sdkgo.FailureValidation, operation, err.Error())}
	}
	return &graphSession{client: client, call: call, operation: operation, credentials: credentials, mailboxRoot: mailboxRoot}, nil
}

// resolveMailboxRoot is /me for a delegated connection and /users/<mailbox> for an app-only one.
func (client *Client) resolveMailboxRoot(authMethodID string) (string, error) {
	switch authMethodID {
	case MicrosoftOAuthAuthMethodID:
		return delegatedRoot, nil
	case AppOnlyAuthMethodID:
		if client.appOnlyMailbox == "" {
			return "", errors.New("an app-only connection needs the mailbox configuration field")
		}
		return "/users/" + url.PathEscape(client.appOnlyMailbox), nil
	default:
		return "", errors.New("Outlook Mail authorization method is not supported")
	}
}

func (client *Client) canRefreshAfterRejection() bool {
	_, supportsRejectionRefresh := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials])
	return supportsRejectionRefresh
}

// exchange sends one request and resends it once after a 401, which Graph answers before running anything.
func (session *graphSession) exchange(request graphRequest) graphExchange {
	result := session.send(request)
	if result.statusCode != http.StatusUnauthorized || session.hasRefreshedAfterRejection || !session.client.canRefreshAfterRejection() {
		return result
	}
	session.hasRefreshedAfterRejection = true
	client := session.client
	replacement, err := sdkgo.ResolveCredentialAfterRejection(session.call.Context, client.credentials, session.call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(replacement) != nil || replacement.AuthMethodID != session.credentials.AuthMethodID {
		return result
	}
	session.credentials = replacement
	return session.send(request)
}

// send performs one HTTP exchange and classifies it without deciding retry policy.
func (session *graphSession) send(request graphRequest) graphExchange {
	httpRequest, err := session.buildHTTPRequest(request)
	if err != nil {
		return graphExchange{outcome: graphDefect, failure: graphFailure(sdkgo.FailureLocalDefect, session.operation, err.Error())}
	}
	httpResponse, err := session.client.httpClient.Do(httpRequest)
	if err != nil {
		if isConnectionNeverEstablished(err) {
			return graphExchange{outcome: graphRetry, failure: graphFailure(sdkgo.FailureTransport, session.operation,
				"Microsoft Graph could not be reached; no request was sent")}
		}
		return graphExchange{outcome: graphAmbiguous, failure: graphFailure(sdkgo.FailureTransport, session.operation,
			"the Microsoft Graph request failed before an answer arrived")}
	}
	return classifyHTTPResponse(session.operation, httpResponse, session.credentials.AccessToken.Reveal(),
		session.client.maxResponseBytes, session.client.now())
}

func (session *graphSession) buildHTTPRequest(request graphRequest) (*http.Request, error) {
	target := request.absoluteURL
	if target == "" {
		target = graphBaseURL + session.mailboxRoot + request.path
		if len(request.query) != 0 {
			target += "?" + request.query.encode()
		}
	}
	var body io.Reader
	switch {
	case request.payload != nil:
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return nil, errRequestNotBuilt
		}
		body = bytes.NewReader(encoded)
	case request.method == http.MethodPost:
		// Graph's send action requires Content-Length: 0, which an empty reader produces.
		body = bytes.NewReader(nil)
	}
	httpRequest, err := http.NewRequestWithContext(session.call.Context, request.method, target, body)
	if err != nil {
		return nil, errRequestNotBuilt
	}
	httpRequest.Header.Set("Authorization", "Bearer "+session.credentials.AccessToken.Reveal())
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Add(preferHeader, immutableIDPreference)
	if request.prefersTextBody {
		httpRequest.Header.Add(preferHeader, textBodyPreference)
	}
	if request.payload != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	return httpRequest, nil
}

func (session *graphSession) receipt(result graphExchange, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: session.call.ID, IdempotencyKey: session.call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ProviderRequestID: result.requestID, ObservedAt: session.client.now().UTC(),
	}
}

// encode writes the parameters in order, escaping spaces as %20 rather than +.
func (query graphQuery) encode() string {
	encoded := make([]string, 0, len(query))
	for _, parameter := range query {
		encoded = append(encoded, escapeQueryComponent(parameter.name)+"="+escapeQueryComponent(parameter.value))
	}
	return strings.Join(encoded, "&")
}

func escapeQueryComponent(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

// classifyHTTPResponse reads a bounded body and maps the status; it closes the body.
func classifyHTTPResponse(operation string, httpResponse *http.Response, secret string, maxResponseBytes int64, now time.Time) graphExchange {
	defer func() {
		// The body is read to its bound below; a close failure cannot change the classified answer.
		_ = httpResponse.Body.Close()
	}()
	result := graphExchange{statusCode: httpResponse.StatusCode}
	if requestID := httpResponse.Header.Get(requestIDHeader); requestIDPattern.MatchString(requestID) {
		result.requestID = requestID
	}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		// An error body is truncated, not rejected; only its machine-readable code is read.
		body, err := io.ReadAll(io.LimitReader(httpResponse.Body, providerhttp.MaxErrorBodyBytes))
		if err != nil {
			body = nil
		}
		result.errorCode = readGraphErrorCode(body, secret)
		result.retryAfter = providerhttp.ParseRetryAfter(httpResponse.Header.Get("Retry-After"), now)
		return classifyFailedStatus(operation, result)
	}
	body, err := providerhttp.ReadBoundedBody(httpResponse.Body, maxResponseBytes)
	switch {
	case errors.Is(err, providerhttp.ErrBodyTooLarge):
		result.outcome = graphInvalid
		result.failure = graphFailure(sdkgo.FailureResponseTooLarge, operation, "the Microsoft Graph response exceeds the configured maxResponseBytes limit")
	case err != nil:
		result.outcome = graphAmbiguous
		result.failure = graphFailure(sdkgo.FailureTransport, operation, "the Microsoft Graph response was interrupted")
	case secret != "" && bytes.Contains(body, []byte(secret)):
		result.outcome = graphInvalid
		result.failure = graphFailure(sdkgo.FailureProtocol, operation, "the Microsoft Graph response reflected the connection credential")
	default:
		result.outcome = graphSucceeded
		result.body = body
	}
	return result
}

// classifyFailedStatus maps a non-2xx answer from its status and error.code; Graph's message text is never read.
func classifyFailedStatus(operation string, result graphExchange) graphExchange {
	status := result.statusCode
	describe := func(outcome graphOutcome, kind sdkgo.FailureKind, message string) graphExchange {
		detail := fmt.Sprintf("%s (HTTP %d)", message, status)
		if result.errorCode != "" {
			detail = fmt.Sprintf("%s (HTTP %d %s)", message, status, result.errorCode)
		}
		result.outcome, result.failure = outcome, graphFailure(kind, operation, detail)
		return result
	}
	switch {
	case status == http.StatusTooManyRequests || status == 509:
		return describe(graphRetry, sdkgo.FailureRateLimit, "Microsoft Graph throttled the request")
	case status == http.StatusRequestTimeout:
		return describe(graphRetry, sdkgo.FailureAvailability, "Microsoft Graph timed out waiting for the request")
	case status == http.StatusConflict || status == http.StatusLocked:
		return describe(graphRetry, sdkgo.FailureConflict, "Microsoft Graph reported a conflicting change to the item")
	case status == http.StatusNotImplemented:
		return describe(graphRejected, sdkgo.FailureProviderRejection, "Microsoft Graph does not implement the request")
	case status == http.StatusInsufficientStorage:
		return describe(graphRejected, sdkgo.FailureQuotaExhausted, "the mailbox is over its storage quota")
	case status >= 500:
		return describe(graphAmbiguous, sdkgo.FailureAvailability, "Microsoft Graph failed or is temporarily unavailable")
	case status == http.StatusUnauthorized:
		return describe(graphRejected, sdkgo.FailureAuthentication, "Microsoft Graph rejected the access token")
	case status == http.StatusForbidden && strings.Contains(result.errorCode, "Quota"):
		return describe(graphRejected, sdkgo.FailureQuotaExhausted, "Microsoft Graph refused the request for a mailbox quota")
	case status == http.StatusForbidden:
		return describe(graphRejected, sdkgo.FailureAuthorization, "Microsoft Graph denied access to the mailbox or item")
	case isMissingItemAnswer(status, result.errorCode):
		return describe(graphNotFound, sdkgo.FailureNotFound, "Microsoft Graph found no such item in the mailbox")
	case status == http.StatusNotFound:
		return describe(graphRejected, sdkgo.FailureNotFound, "Microsoft Graph found no such mailbox or resource")
	case status >= 300 && status < 400:
		return describe(graphRejected, sdkgo.FailureProtocol, "Microsoft Graph redirected the request")
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity || status == http.StatusRequestEntityTooLarge:
		return describe(graphRejected, sdkgo.FailureValidation, "Microsoft Graph rejected the request")
	default:
		return describe(graphRejected, sdkgo.FailureProviderRejection, "Microsoft Graph rejected the request")
	}
}

// isMissingItemAnswer recognizes Exchange's answers for an item that does not exist or an ID that cannot name one.
func isMissingItemAnswer(status int, errorCode string) bool {
	switch status {
	case http.StatusNotFound:
		return errorCode == "" || errorCode == "ErrorItemNotFound"
	case http.StatusBadRequest:
		return errorCode == "ErrorInvalidIdMalformed"
	default:
		return false
	}
}

// readGraphErrorCode reads error.code, or the inner code when the outer one is missing.
func readGraphErrorCode(body []byte, secret string) string {
	for _, token := range providerhttp.ReadErrorTokens(body, []string{"/error/code", "/error/innerError/code"}) {
		if secret == "" || !strings.Contains(token, secret) {
			return token
		}
	}
	return ""
}

// queryBranches names a Query's branches for the shared read outcome mapping.
type queryBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// queryAttemptForExchange maps every non-success outcome; reads repeat safely after any server error.
func queryAttemptForExchange[OUT any](result graphExchange, receipt sdkgo.Receipt, branches queryBranches) (sdkgo.QueryAttempt[OUT], bool) {
	var empty OUT
	switch result.outcome {
	case graphSucceeded:
		return sdkgo.QueryAttempt[OUT]{}, false
	case graphRetry, graphAmbiguous:
		return sdkgo.NewQueryRetry[OUT](result.failure, result.retryAfter), true
	case graphNotFound:
		if branches.notFound != "" {
			return sdkgo.NewQueryBranch(branches.notFound, empty, &result.failure, receipt), true
		}
		return sdkgo.NewQueryBranch(branches.providerRejected, empty, &result.failure, receipt), true
	case graphInvalid:
		return sdkgo.NewQueryBranch(branches.invalidResponse, empty, &result.failure, receipt), true
	case graphDefect:
		return sdkgo.NewQueryBranch(branches.defect, empty, &result.failure, receipt), true
	default:
		return sdkgo.NewQueryBranch(branches.providerRejected, empty, &result.failure, receipt), true
	}
}

// updateBranches names a moveMessage or setMessageFlags branch set for the shared write outcome mapping.
type updateBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
}

// updateAttemptForExchange retries every unconfirmed outcome, because a move or an absolute flag change
// is safe to repeat after a fresh read.
func updateAttemptForExchange[OUT any](result graphExchange, receipt sdkgo.Receipt, branches updateBranches) (sdkgo.MutationAttempt[OUT], bool) {
	var empty OUT
	switch result.outcome {
	case graphSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case graphRetry, graphAmbiguous, graphInvalid:
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case graphNotFound:
		return sdkgo.NewMutationBranch(branches.notFound, empty, &result.failure, receipt), true
	case graphDefect:
		return sdkgo.NewMutationBranch(branches.defect, empty, &result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(branches.providerRejected, empty, &result.failure, receipt), true
	}
}

// validateMailbox accepts an SMTP address or user principal name, or an Entra object ID.
func validateMailbox(mailbox string) error {
	if guidPattern.MatchString(mailbox) {
		return nil
	}
	parsed, err := mail.ParseAddress(mailbox)
	if err != nil || parsed.Name != "" || parsed.Address != mailbox || strings.ContainsAny(mailbox, "/?#%'\\") {
		return errors.New("configuration mailbox must be an address such as support@contoso.com or an Entra object ID")
	}
	return nil
}

// validateNextPageLink accepts only a Graph v1.0 messages collection link, so a cursor cannot redirect the token.
func validateNextPageLink(link string) error {
	if link == "" || len(link) > maximumNextPageLinkBytes {
		return errors.New("next-page link is empty or too long")
	}
	parsed, err := url.Parse(link)
	switch {
	case err != nil:
		return errors.New("next-page link cannot be parsed")
	case parsed.Scheme != "https" || parsed.Host != graphHost || parsed.User != nil || parsed.Fragment != "":
		return errors.New("next-page link is not an https://graph.microsoft.com link")
	case !strings.HasPrefix(parsed.Path, graphAPIVersion+"/") || !strings.HasSuffix(strings.ToLower(parsed.Path), "/messages"):
		return errors.New("next-page link is not a Microsoft Graph v1.0 messages collection")
	case strings.Contains(parsed.Path, ".."):
		return errors.New("next-page link contains a relative path segment")
	}
	return nil
}

// isConnectionNeverEstablished reports a dial failure, after which Graph cannot have received the request.
func isConnectionNeverEstablished(err error) bool {
	var operationError *net.OpError
	return errors.As(err, &operationError) && operationError.Op == "dial"
}

func graphFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func graphFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := graphFailure(kind, operation, message)
	return &failure
}
