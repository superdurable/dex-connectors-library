// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package docusign connects Dex applications to the DocuSign eSignature REST API v2.1.
//
// createEnvelopeFromTemplate sends an envelope from a server template, getEnvelope and
// listEnvelopeRecipients read its progress, voidEnvelope voids it as compensation, and
// downloadCombinedDocument streams the combined PDF through a SHA-256 digest into an optional
// application-owned CombinedDocumentStore, so the PDF never enters a Dex payload. The
// envelopeEventReceived Trigger serves one Connect endpoint per connection, verifies each
// X-DocuSign-Signature, and records every envelope-completed, envelope-declined, and
// envelope-voided event in each accepting binding's durable inbox before answering 200.
//
// A connection authorizes one DocuSign user through the Authorization Code Grant against the
// production or the developer environment. Each call reads the user's account and regional base URI
// from /oauth/userinfo, accepts only a DocuSign host for that environment, and caches it for the
// access token's life. The runnable examples/envelope-signing application sends an envelope, waits
// durably for Connect or a status poll, and records the signed document's digest.
package docusign

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// ProductionOAuthAuthMethodID identifies a connection authorized against account.docusign.com.
	ProductionOAuthAuthMethodID = "docusign-oauth"
	// DeveloperOAuthAuthMethodID identifies a connection authorized against account-d.docusign.com.
	DeveloperOAuthAuthMethodID = "docusign-developer-oauth"

	// docusignHTTPTimeout bounds every request; each operation sets a shorter request deadline.
	docusignHTTPTimeout = 115 * time.Second
	// docusignRequestTimeout leaves time within a 30-second Execute timeout to classify a response.
	docusignRequestTimeout = 25 * time.Second
	// accountResolutionLifetime bounds how long a /oauth/userinfo answer is reused for one access token.
	accountResolutionLifetime = time.Hour
	// maximumCachedAccountResolutions bounds the userinfo cache across connections and rotated tokens.
	maximumCachedAccountResolutions = 256
)

var (
	errDocuSignRequestInvalid    = errors.New("DocuSign request could not be built")
	errDocuSignResponseTooLarge  = errors.New("DocuSign response exceeds the configured maxResponseBytes")
	errDocuSignResponseMalformed = errors.New("DocuSign returned a malformed response")

	errDocuSignReauthorizationRequired = errors.New("the DocuSign connection needs reauthorization; authorize it again in Dex Web Connections")
	errDocuSignCredentialsUnavailable  = errors.New("the DocuSign access token could not be loaded or refreshed yet")
	errDocuSignAccessTokenUnusable     = errors.New("the DocuSign access token is blank or contains characters a header cannot carry")
	errDocuSignAuthMethodUnknown       = errors.New("the DocuSign connection names no known environment; authorize it again in Dex Web Connections")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient    *http.Client
	now           func() time.Time
	logger        *slog.Logger
	documentStore CombinedDocumentStore
}

// WithHTTPClient replaces the HTTP client used for DocuSign API, userinfo, and token requests. The
// connector copies it, never follows a redirect, and applies a 115-second timeout when the client sets
// none; each operation also bounds its own request.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithClock replaces the clock used for receipts, credential expiry, rate-limit delays, and the
// userinfo cache.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// WithLogger sends the records of the Connect endpoint and of the durable inboxes that
// NewProjectEnvelopeEventReceivedEndpointRunner creates to logger. Without it, those records go to
// slog.Default(). Records carry event IDs only.
func WithLogger(logger *slog.Logger) Option {
	return func(options *clientOptions) { options.logger = logger }
}

// WithCombinedDocumentStore makes downloadCombinedDocument stream every combined PDF into store
// while it computes the digest. Without it, the operation records only the byte count and digest.
// The store is application code; see CombinedDocumentStore for its contract.
func WithCombinedDocumentStore(store CombinedDocumentStore) Option {
	return func(options *clientOptions) { options.documentStore = store }
}

// Client executes authenticated DocuSign eSignature calls and receives signed Connect deliveries for
// one connection configuration. It is safe for concurrent use.
type Client struct {
	credentials         CredentialSource
	refreshDriver       *CredentialRefreshDriver
	httpClient          *http.Client
	now                 func() time.Time
	logger              *slog.Logger
	documentStore       CombinedDocumentStore
	configuredAccountID string
	maxResponseBytes    int64
	maxDocumentBytes    int64
	connectMaxBodyBytes int64

	accountsMu sync.Mutex
	accounts   map[accountResolutionKey]resolvedAccount

	envelopeEventEndpointsMu sync.Mutex
	envelopeEventEndpoints   map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, EnvelopeEvent]
}

// accountResolutionKey ties a cached account to the access token that read it.
type accountResolutionKey struct {
	connection        sdkgo.ConnectionRef
	accessTokenDigest [sha256.Size]byte
}

// docusignSession is one call's credentials and the account its API requests address.
type docusignSession struct {
	credentials Credentials
	account     resolvedAccount
}

// sessionFailureKind says how an operation reports a failure to open its session.
type sessionFailureKind int

const (
	sessionFailureRetry sessionFailureKind = iota + 1
	sessionFailureDefect
	sessionFailureRejected
	sessionFailureInvalidResponse
)

// sessionFailure is why a call lacked credentials or its account; this call sent nothing to eSignature.
type sessionFailure struct {
	kind       sessionFailureKind
	retryAfter time.Duration
	failure    sdkgo.Failure
}

// docusignRequest is one eSignature REST request below /restapi/v2.1/accounts/{accountId}.
type docusignRequest struct {
	method  string
	path    string
	query   url.Values
	body    any
	timeout time.Duration
}

// docusignResponse is one bounded DocuSign answer. A 2xx body is read up to maxResponseBytes.
type docusignResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

// docusignOutcome classifies a non-2xx answer: a Retry, or a conclusive failure the caller maps to a branch.
type docusignOutcome struct {
	isRetry    bool
	isNotFound bool
	retryAfter time.Duration
	errorCode  string
	failure    sdkgo.Failure
}

// New validates config and constructs a Client. Blank configuration fields take their manifest defaults.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.AccountID != "" && !isDocuSignGUID(config.AccountID) {
		return nil, fmt.Errorf("configuration accountId must be a DocuSign API account ID GUID, such as 00000000-0000-0000-0000-000000000000")
	}
	if credentials == nil {
		return nil, fmt.Errorf("DocuSign credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("DocuSign connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, fmt.Errorf("DocuSign connector clock is required")
	}
	httpClient := providerhttp.NewProviderHTTPClient(dependencies.httpClient, docusignHTTPTimeout)
	refreshDriver := newCredentialRefreshDriver(httpClient, dependencies.now)
	return &Client{
		credentials: credentials, refreshDriver: refreshDriver, httpClient: httpClient,
		now: dependencies.now, logger: dependencies.logger, documentStore: dependencies.documentStore,
		configuredAccountID: config.AccountID, maxResponseBytes: config.MaxResponseBytes,
		maxDocumentBytes: config.MaxDocumentBytes, connectMaxBodyBytes: config.ConnectMaxBodyBytes,
		accounts:               make(map[accountResolutionKey]resolvedAccount),
		envelopeEventEndpoints: make(map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, EnvelopeEvent]),
	}, nil
}

// CreateEnvelopeFromTemplate returns the createEnvelopeFromTemplate Mutation bound to this client.
func (client *Client) CreateEnvelopeFromTemplate() CreateEnvelopeFromTemplateOperation {
	return CreateEnvelopeFromTemplateOperation{client: client}
}

// GetEnvelope returns the getEnvelope Query bound to this client.
func (client *Client) GetEnvelope() GetEnvelopeOperation {
	return GetEnvelopeOperation{client: client}
}

// ListEnvelopeRecipients returns the listEnvelopeRecipients Query bound to this client.
func (client *Client) ListEnvelopeRecipients() ListEnvelopeRecipientsOperation {
	return ListEnvelopeRecipientsOperation{client: client}
}

// VoidEnvelope returns the voidEnvelope Mutation bound to this client.
func (client *Client) VoidEnvelope() VoidEnvelopeOperation {
	return VoidEnvelopeOperation{client: client}
}

// DownloadCombinedDocument returns the downloadCombinedDocument Query bound to this client.
func (client *Client) DownloadCombinedDocument() DownloadCombinedDocumentOperation {
	return DownloadCombinedDocumentOperation{client: client}
}

// openSession resolves credentials and the connection's account before any eSignature request.
func (client *Client) openSession(call sdkgo.Call, operationID string) (docusignSession, *sessionFailure) {
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		if errors.Is(err, errDocuSignCredentialsUnavailable) {
			return docusignSession{}, &sessionFailure{kind: sessionFailureRetry, failure: docusignFailure(operationID, sdkgo.FailureAvailability, err.Error())}
		}
		return docusignSession{}, &sessionFailure{kind: sessionFailureDefect, failure: docusignFailure(operationID, sdkgo.FailureAuthentication, err.Error())}
	}
	environment, isKnown := docusignEnvironmentFor(credentials.AuthMethodID)
	if !isKnown {
		return docusignSession{}, &sessionFailure{kind: sessionFailureDefect, failure: docusignFailure(operationID, sdkgo.FailureLocalDefect, errDocuSignAuthMethodUnknown.Error())}
	}
	account, failure := client.resolveAccount(call, operationID, &credentials, environment)
	if failure != nil {
		return docusignSession{}, failure
	}
	return docusignSession{credentials: credentials, account: account}, nil
}

// resolveCredentials refreshes an expiring access token before use; a failure never names the cause.
func (client *Client) resolveCredentials(call sdkgo.Call) (Credentials, error) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return Credentials{}, errDocuSignReauthorizationRequired
	case err != nil:
		return Credentials{}, errDocuSignCredentialsUnavailable
	case !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()):
		return Credentials{}, errDocuSignAccessTokenUnusable
	}
	return credentials, nil
}

// resolveAccount returns the cached account for this access token, or reads /oauth/userinfo once.
func (client *Client) resolveAccount(
	call sdkgo.Call, operationID string, credentials *Credentials, environment docusignEnvironment,
) (resolvedAccount, *sessionFailure) {
	if account, isCached := client.cachedAccount(call.Connection, *credentials); isCached {
		return account, nil
	}
	target := environment.accountServerURL + "/oauth/userinfo"
	requestContext, cancel := context.WithTimeout(call.Context, docusignRequestTimeout)
	defer cancel()
	response, err := client.sendWithRejectionRefresh(call, requestContext, credentials, http.MethodGet, target, nil, client.maxResponseBytes, nil)
	switch {
	case errors.Is(err, errDocuSignResponseTooLarge):
		return resolvedAccount{}, &sessionFailure{kind: sessionFailureInvalidResponse, failure: docusignFailure(operationID, sdkgo.FailureResponseTooLarge, "DocuSign account information exceeds the configured maxResponseBytes")}
	case err != nil:
		return resolvedAccount{}, &sessionFailure{kind: sessionFailureRetry, failure: docusignFailure(operationID, sdkgo.FailureTransport, "DocuSign account information could not be read")}
	case response.statusCode == http.StatusTooManyRequests:
		return resolvedAccount{}, &sessionFailure{kind: sessionFailureRetry, retryAfter: untilNextHour(client.now()),
			failure: docusignFailure(operationID, sdkgo.FailureRateLimit, "DocuSign limited the hourly account information requests for this user or integration key")}
	case response.statusCode >= 500 || response.statusCode == http.StatusRequestTimeout:
		return resolvedAccount{}, &sessionFailure{kind: sessionFailureRetry, failure: docusignFailure(operationID, sdkgo.FailureAvailability, fmt.Sprintf("DocuSign account information answered HTTP %d", response.statusCode))}
	case response.statusCode == http.StatusUnauthorized:
		return resolvedAccount{}, &sessionFailure{kind: sessionFailureRejected, failure: docusignFailure(operationID, sdkgo.FailureAuthentication, "DocuSign rejected the access token; authorize the connection again")}
	case response.statusCode != http.StatusOK:
		return resolvedAccount{}, &sessionFailure{kind: sessionFailureRejected, failure: docusignFailure(operationID, sdkgo.FailureProviderRejection, fmt.Sprintf("DocuSign account information answered HTTP %d", response.statusCode))}
	}
	account, err := selectAccount(response.body, client.configuredAccountID, environment)
	if err != nil {
		if errors.Is(err, errDocuSignResponseMalformed) {
			return resolvedAccount{}, &sessionFailure{kind: sessionFailureInvalidResponse, failure: docusignFailure(operationID, sdkgo.FailureProtocol, err.Error())}
		}
		return resolvedAccount{}, &sessionFailure{kind: sessionFailureDefect, failure: docusignFailure(operationID, sdkgo.FailureValidation, err.Error())}
	}
	client.storeAccount(call.Connection, *credentials, account)
	return account, nil
}

// cachedAccount returns an unexpired account read with exactly this access token.
func (client *Client) cachedAccount(connection sdkgo.ConnectionRef, credentials Credentials) (resolvedAccount, bool) {
	key := newAccountResolutionKey(connection, credentials)
	client.accountsMu.Lock()
	defer client.accountsMu.Unlock()
	account, isFound := client.accounts[key]
	if !isFound || client.now().Sub(account.resolvedAt) >= accountResolutionLifetime {
		return resolvedAccount{}, false
	}
	return account, true
}

// storeAccount records account and drops expired entries, keeping the cache bounded.
func (client *Client) storeAccount(connection sdkgo.ConnectionRef, credentials Credentials, account resolvedAccount) {
	account.resolvedAt = client.now()
	client.accountsMu.Lock()
	defer client.accountsMu.Unlock()
	for key, cached := range client.accounts {
		if account.resolvedAt.Sub(cached.resolvedAt) >= accountResolutionLifetime || len(client.accounts) >= maximumCachedAccountResolutions {
			delete(client.accounts, key)
		}
	}
	client.accounts[newAccountResolutionKey(connection, credentials)] = account
}

// sendAPIRequest sends one JSON eSignature request; isDispatched records whether the request was written.
func (client *Client) sendAPIRequest(
	call sdkgo.Call, session *docusignSession, request docusignRequest, isDispatched *atomic.Bool,
) (docusignResponse, error) {
	var encodedBody []byte
	if request.body != nil {
		encoded, err := json.Marshal(request.body)
		if err != nil {
			return docusignResponse{}, errDocuSignRequestInvalid
		}
		encodedBody = encoded
	}
	requestContext, cancel := context.WithTimeout(call.Context, cmp.Or(request.timeout, docusignRequestTimeout))
	defer cancel()
	return client.sendWithRejectionRefresh(call, requestContext, &session.credentials, request.method,
		session.account.apiURL(request.path, request.query), encodedBody, client.maxResponseBytes, isDispatched)
}

// sendWithRejectionRefresh repeats a request once after a 401 and a forced token refresh.
func (client *Client) sendWithRejectionRefresh(
	call sdkgo.Call, requestContext context.Context, credentials *Credentials, method string, target string,
	body []byte, maxBodyBytes int64, isDispatched *atomic.Bool,
) (docusignResponse, error) {
	for attempt := 0; attempt < 2; attempt++ {
		response, err := client.sendOnce(requestContext, credentials.AccessToken.Reveal(), method, target, jsonMediaType, body, isDispatched)
		if err != nil {
			return docusignResponse{}, err
		}
		if response.StatusCode != http.StatusUnauthorized || attempt > 0 || !client.canRefreshAfterRejection() {
			return client.readResponse(response, maxBodyBytes)
		}
		rejected, err := client.readResponse(response, maxBodyBytes)
		if err != nil {
			return rejected, err
		}
		replacement, refreshErr := sdkgo.ResolveCredentialAfterRejection(requestContext, client.credentials, call, client.refreshDriver)
		if refreshErr != nil || !providerhttp.IsHeaderSafeCredential(replacement.AccessToken.Reveal()) {
			return rejected, nil
		}
		*credentials = replacement
		if isDispatched != nil {
			// DocuSign refused the first request unprocessed, so only the repeat can leave an unknown outcome.
			isDispatched.Store(false)
		}
	}
	return docusignResponse{}, errors.New("DocuSign authenticated request retry was exhausted")
}

// openDocumentStream returns an open 2xx download, or the bounded error response.
func (client *Client) openDocumentStream(
	call sdkgo.Call, requestContext context.Context, session *docusignSession, path string, query url.Values,
) (*http.Response, docusignResponse, error) {
	target := session.account.apiURL(path, query)
	for attempt := 0; attempt < 2; attempt++ {
		response, err := client.sendOnce(requestContext, session.credentials.AccessToken.Reveal(), http.MethodGet, target, pdfMediaType, nil, nil)
		if err != nil {
			return nil, docusignResponse{}, err
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, docusignResponse{}, nil
		}
		rejected, err := client.readResponse(response, 0)
		if err != nil || rejected.statusCode != http.StatusUnauthorized || attempt > 0 || !client.canRefreshAfterRejection() {
			return nil, rejected, err
		}
		replacement, refreshErr := sdkgo.ResolveCredentialAfterRejection(requestContext, client.credentials, call, client.refreshDriver)
		if refreshErr != nil || !providerhttp.IsHeaderSafeCredential(replacement.AccessToken.Reveal()) {
			return nil, rejected, nil
		}
		session.credentials = replacement
	}
	return nil, docusignResponse{}, errors.New("DocuSign authenticated request retry was exhausted")
}

func (client *Client) sendOnce(
	requestContext context.Context, accessToken string, method string, target string, accept string, body []byte, isDispatched *atomic.Bool,
) (*http.Response, error) {
	tracedContext := httptrace.WithClientTrace(requestContext, &httptrace.ClientTrace{
		WroteRequest: func(written httptrace.WroteRequestInfo) {
			if written.Err == nil && isDispatched != nil {
				isDispatched.Store(true)
			}
		},
	})
	var requestBody io.Reader
	if body != nil {
		requestBody = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(tracedContext, method, target, requestBody)
	if err != nil {
		return nil, errDocuSignRequestInvalid
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Accept", accept)
	if body != nil {
		request.Header.Set("Content-Type", jsonMediaType)
	}
	return client.httpClient.Do(request)
}

// readResponse reads a 2xx body within maxBodyBytes and an error body within the shared error limit.
func (client *Client) readResponse(response *http.Response, maxBodyBytes int64) (docusignResponse, error) {
	defer response.Body.Close()
	result := docusignResponse{statusCode: response.StatusCode, header: response.Header.Clone()}
	if response.StatusCode < 200 || response.StatusCode >= 300 || maxBodyBytes <= 0 {
		// Only the errorCode is read, so a truncated or unreadable error body keeps its status.
		result.body, _ = io.ReadAll(io.LimitReader(response.Body, providerhttp.MaxErrorBodyBytes))
		return result, nil
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, maxBodyBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return result, errDocuSignResponseTooLarge
	}
	result.body = body
	return result, err
}

// canRefreshAfterRejection is true when the credential provider can force one refresh after a 401.
func (client *Client) canRefreshAfterRejection() bool {
	_, isRefreshing := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials])
	return isRefreshing
}

// classifyDocuSignFailure maps errorCode, then status; DocuSign's message text never reaches a Failure.
func (client *Client) classifyDocuSignFailure(operationID string, response docusignResponse) docusignOutcome {
	errorCode := readDocuSignErrorCode(response.body)
	status := response.statusCode
	outcome := docusignOutcome{errorCode: errorCode}
	switch {
	case isDocuSignRateLimitCode(errorCode) || status == http.StatusTooManyRequests:
		outcome.isRetry = true
		outcome.retryAfter = docusignRateLimitDelay(errorCode, response.header, client.now())
		outcome.failure = docusignFailure(operationID, sdkgo.FailureRateLimit, describeDocuSignError("DocuSign rate limited the request", errorCode))
	case status >= 500 || status == http.StatusRequestTimeout:
		outcome.isRetry = true
		outcome.failure = docusignFailure(operationID, sdkgo.FailureAvailability, describeDocuSignError(fmt.Sprintf("DocuSign answered HTTP %d", status), errorCode))
	case strings.EqualFold(errorCode, "ENVELOPE_ALLOWANCE_EXCEEDED"):
		outcome.failure = docusignFailure(operationID, sdkgo.FailureQuotaExhausted, describeDocuSignError("the DocuSign account has no envelope allowance left", errorCode))
	case status == http.StatusUnauthorized || isDocuSignCodeIn(errorCode, docusignAuthenticationCodes):
		outcome.failure = docusignFailure(operationID, sdkgo.FailureAuthentication, describeDocuSignError("DocuSign rejected the access token; authorize the connection again", errorCode))
	case status == http.StatusForbidden || isDocuSignCodeIn(errorCode, docusignAuthorizationCodes):
		outcome.failure = docusignFailure(operationID, sdkgo.FailureAuthorization, describeDocuSignError("DocuSign denied permission for this account, user, or envelope", errorCode))
	case status == http.StatusNotFound || isDocuSignCodeIn(errorCode, docusignNotFoundCodes):
		outcome.isNotFound = true
		outcome.failure = docusignFailure(operationID, sdkgo.FailureNotFound, describeDocuSignError("the DocuSign envelope or document was not found", errorCode))
	default:
		outcome.failure = docusignFailure(operationID, sdkgo.FailureProviderRejection, describeDocuSignError(fmt.Sprintf("DocuSign rejected the request with HTTP %d", status), errorCode))
	}
	return outcome
}

// receipt names the envelope and, for a rejection, DocuSign's errorCode.
func (client *Client) receipt(envelopeID string, errorCode string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{Provider: ConnectorID, ProviderObjectID: envelopeID, ObservedAt: client.now().UTC()}
	if errorCode != "" {
		receipt.Metadata = map[string]string{"errorCode": errorCode}
	}
	return receipt
}
