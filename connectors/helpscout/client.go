// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package helpscout connects Dex applications to the Help Scout Inbox API 2.0.
//
// Queries search conversations, read one conversation with its newest threads, and find customer
// profiles by email. Mutations add a customer reply or an internal note, and change a conversation's
// status, assignee, and tags. The conversationEvent Trigger serves one webhook endpoint per connection,
// verifies every X-HelpScout-Signature, and records each conversation event in every accepting binding's
// durable inbox before answering 200.
//
// A connection holds the App ID and App Secret of a Help Scout app. The connector obtains a two-day access
// token from them with the client credentials grant through sdkgo/oauthtoken, stores it, and renews it
// when it expires; the generated NewProjectConnection builds such a connection. Conversation statuses are Help
// Scout's own active, pending, closed, and spam, never remapped. The runnable examples/conversation-triage
// application starts one Flow per new conversation and uses every operation.
package helpscout

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// helpScoutAPIBaseURL is the only host the connector sends an access token to.
	helpScoutAPIBaseURL = "https://api.helpscout.net"
	// helpScoutRequestTimeout leaves time within the 30-second Execute timeout to classify a response.
	helpScoutRequestTimeout = 25 * time.Second
	// rateLimitRetryAfterHeader is the seconds-to-wait header Help Scout documents for a 429.
	rateLimitRetryAfterHeader = "X-RateLimit-Retry-After"
	// resourceIDHeader carries the ID of a thread Help Scout created.
	resourceIDHeader = "Resource-Id"

	maximumReportedErrorPaths = 5
)

var (
	errorPathPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.\[\]]{0,63}$`)
	errorCodePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,63}$`)
	logRefPattern    = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

	errHelpScoutRequestInvalid    = errors.New("Help Scout request could not be built")
	errHelpScoutResponseTooLarge  = errors.New("Help Scout response exceeds the configured maxResponseBytes")
	errHelpScoutResponseMalformed = errors.New("Help Scout returned a malformed response")

	errHelpScoutReauthorizationRequired = errors.New("Help Scout rejected the App ID or App Secret; save the app's current credentials in Dex Web Connections")
	errHelpScoutCredentialsUnavailable  = errors.New("the Help Scout access token could not be loaded or obtained yet")
	errHelpScoutAccessTokenUnusable     = errors.New("the Help Scout access token is blank or contains characters a header cannot carry")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
	logger     *slog.Logger
}

// WithHTTPClient replaces the HTTP client used for Help Scout API and token requests. The connector copies
// it, never follows a redirect, and applies a 25-second timeout when the client sets none.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithClock replaces the clock used for receipts, webhook receipt times, and credential expiry.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// WithLogger sends the webhook endpoint's delivery records and those of the durable inboxes that
// NewProjectConversationEventEndpointRunner creates to logger. Without it, those records go to
// slog.Default(). Records carry event IDs, never bodies or secrets.
func WithLogger(logger *slog.Logger) Option {
	return func(options *clientOptions) { options.logger = logger }
}

// Client executes authenticated Help Scout Inbox API calls and receives signed Help Scout webhooks for one
// connection configuration. It is safe for concurrent use.
type Client struct {
	credentials         CredentialSource
	refreshDriver       *CredentialRefreshDriver
	httpClient          *http.Client
	now                 func() time.Time
	logger              *slog.Logger
	maxResponseBytes    int64
	webhookMaxBodyBytes int64

	conversationEventEndpointsMu sync.Mutex
	conversationEventEndpoints   map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, ConversationEvent]
}

// helpScoutRequest is one Inbox API request below helpScoutAPIBaseURL.
type helpScoutRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

// helpScoutResponse is one bounded Help Scout answer. A 2xx body is read up to maxResponseBytes.
type helpScoutResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

// helpScoutOutcome classifies a non-2xx answer: a Retry, or a conclusive failure the caller maps to a branch.
type helpScoutOutcome struct {
	isRetry    bool
	retryAfter time.Duration
	failure    sdkgo.Failure
	logRef     string
}

// helpScoutErrorSummary holds only the machine-readable parts of a Help Scout error object.
type helpScoutErrorSummary struct {
	logRef string
	fields []string
}

// New validates config and constructs a Client. Blank configuration fields take their manifest defaults.
// Credentials are resolved through sdkgo.ResolveCredential and the client's CredentialRefreshDriver before
// every provider call, so credentials must come from a source that can store the obtained access token,
// such as the one the generated NewProjectConnection builds.
// Replaced credentials take effect without a restart; the response limits are startup configuration.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.MaxResponseBytes < 1 || config.WebhookMaxBodyBytes < 1 {
		return nil, errors.New("Help Scout maxResponseBytes and webhookMaxBodyBytes must be positive")
	}
	if credentials == nil {
		return nil, errors.New("Help Scout credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Help Scout connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("Help Scout connector clock is required")
	}
	httpClient := providerhttp.NewProviderHTTPClient(dependencies.httpClient, helpScoutRequestTimeout)
	refreshDriver := NewCredentialRefreshDriver(httpClient)
	refreshDriver.now = dependencies.now
	return &Client{
		credentials: credentials, refreshDriver: refreshDriver, httpClient: httpClient,
		now: dependencies.now, logger: dependencies.logger,
		maxResponseBytes: config.MaxResponseBytes, webhookMaxBodyBytes: config.WebhookMaxBodyBytes,
		conversationEventEndpoints: make(map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, ConversationEvent]),
	}, nil
}

// SearchConversations returns the searchConversations Query bound to this client.
func (client *Client) SearchConversations() SearchConversationsOperation {
	return SearchConversationsOperation{client: client}
}

// GetConversation returns the getConversation Query bound to this client.
func (client *Client) GetConversation() GetConversationOperation {
	return GetConversationOperation{client: client}
}

// ReplyToConversation returns the replyToConversation Mutation bound to this client.
func (client *Client) ReplyToConversation() ReplyToConversationOperation {
	return ReplyToConversationOperation{client: client}
}

// UpdateConversation returns the updateConversation Mutation bound to this client.
func (client *Client) UpdateConversation() UpdateConversationOperation {
	return UpdateConversationOperation{client: client}
}

// FindCustomerByEmail returns the findCustomerByEmail Query bound to this client.
func (client *Client) FindCustomerByEmail() FindCustomerByEmailOperation {
	return FindCustomerByEmailOperation{client: client}
}

// resolveCredentials obtains or renews the access token before use; a failure never names the cause.
func (client *Client) resolveCredentials(call sdkgo.Call) (Credentials, error) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return Credentials{}, errHelpScoutReauthorizationRequired
	case err != nil:
		return Credentials{}, errHelpScoutCredentialsUnavailable
	case !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()):
		return Credentials{}, errHelpScoutAccessTokenUnusable
	}
	return credentials, nil
}

// send repeats once after a 401 and a new token; isDispatched reports a possibly applied write.
func (client *Client) send(
	requestContext context.Context, call sdkgo.Call, credentials *Credentials, request helpScoutRequest, isDispatched *atomic.Bool,
) (helpScoutResponse, error) {
	var encodedBody []byte
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return helpScoutResponse{}, errHelpScoutRequestInvalid
		}
		encodedBody = encoded
	}
	target := helpScoutAPIBaseURL + request.path
	if len(request.query) > 0 {
		target += "?" + request.query.Encode()
	}
	for attempt := 0; attempt < 2; attempt++ {
		response, err := client.sendOnce(requestContext, credentials.AccessToken.Reveal(), request.method, target, encodedBody, isDispatched)
		if err != nil || response.statusCode != http.StatusUnauthorized || attempt > 0 || !client.canRefreshAfterRejection() {
			return response, err
		}
		replacement, refreshErr := sdkgo.ResolveCredentialAfterRejection(requestContext, client.credentials, call, client.refreshDriver)
		if refreshErr != nil || !providerhttp.IsHeaderSafeCredential(replacement.AccessToken.Reveal()) {
			return response, nil
		}
		*credentials = replacement
		if isDispatched != nil {
			// Help Scout refused the first request unprocessed, so only the repeat can leave an unknown outcome.
			isDispatched.Store(false)
		}
	}
	return helpScoutResponse{}, errors.New("Help Scout authenticated request retry was exhausted")
}

func (client *Client) sendOnce(
	requestContext context.Context, accessToken string, method string, target string, body []byte, isDispatched *atomic.Bool,
) (helpScoutResponse, error) {
	if requestContext == nil {
		requestContext = context.Background()
	}
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
	httpRequest, err := http.NewRequestWithContext(tracedContext, method, target, requestBody)
	if err != nil {
		return helpScoutResponse{}, errHelpScoutRequestInvalid
	}
	httpRequest.Header.Set("Authorization", "Bearer "+accessToken)
	httpRequest.Header.Set("Accept", "application/json")
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json; charset=UTF-8")
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return helpScoutResponse{}, err
	}
	defer httpResponse.Body.Close()
	response := helpScoutResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		// Only error tokens are read, so a truncated or unreadable error body keeps its status.
		response.body, _ = io.ReadAll(io.LimitReader(httpResponse.Body, providerhttp.MaxErrorBodyBytes))
		return response, nil
	}
	response.body, err = providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return response, errHelpScoutResponseTooLarge
	}
	if err != nil {
		return response, err
	}
	if accessToken != "" && bytes.Contains(response.body, []byte(accessToken)) {
		return helpScoutResponse{statusCode: response.statusCode, header: response.header}, errHelpScoutResponseMalformed
	}
	return response, nil
}

// canRefreshAfterRejection is false for a provider, such as a static test provider, that cannot force a refresh.
func (client *Client) canRefreshAfterRejection() bool {
	_, isRefreshing := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials])
	return isRefreshing
}

// classifyFailure retries 408, 429, and 5xx; every message is the connector's, never Help Scout's.
func (client *Client) classifyFailure(operationID string, response helpScoutResponse, credentials Credentials) helpScoutOutcome {
	summary := describeHelpScoutError(response.body, credentials.AccessToken.Reveal())
	status := response.statusCode
	outcome := helpScoutOutcome{logRef: summary.logRef}
	switch {
	case status == http.StatusTooManyRequests:
		outcome.isRetry, outcome.retryAfter = true, helpScoutRateLimitDelay(response.header, client.now())
		outcome.failure = helpScoutFailure(operationID, sdkgo.FailureRateLimit, "Help Scout rate limited the request (HTTP 429)")
	case status == http.StatusRequestTimeout || status >= 500:
		outcome.isRetry = true
		outcome.failure = helpScoutFailure(operationID, sdkgo.FailureAvailability, fmt.Sprintf("Help Scout could not complete the request yet (HTTP %d)", status))
	case status == http.StatusUnauthorized:
		outcome.failure = helpScoutFailure(operationID, sdkgo.FailureAuthentication,
			"Help Scout rejected the access token (HTTP 401); check that the app's owner is an active Help Scout user")
	case status == http.StatusForbidden:
		outcome.failure = helpScoutFailure(operationID, sdkgo.FailureAuthorization, withErrorSummary(
			"Help Scout denied access (HTTP 403), for example because the API is not enabled for the account, payment is missing, or the user cannot access the inbox", summary))
	case status == http.StatusNotFound || status == http.StatusGone:
		outcome.failure = helpScoutFailure(operationID, sdkgo.FailureNotFound, fmt.Sprintf("Help Scout found no such resource (HTTP %d)", status))
	case status >= 300 && status < 400:
		outcome.failure = helpScoutFailure(operationID, sdkgo.FailureProtocol, fmt.Sprintf("Help Scout redirected the request (HTTP %d), which the connector never follows", status))
	case status == http.StatusConflict:
		outcome.failure = helpScoutFailure(operationID, sdkgo.FailureConflict, withErrorSummary("Help Scout reported a conflicting resource (HTTP 409)", summary))
	case status == http.StatusPreconditionFailed || status == http.StatusLocked:
		outcome.failure = helpScoutFailure(operationID, sdkgo.FailureProviderRejection, withErrorSummary(fmt.Sprintf(
			"Help Scout refused to change a locked conversation or customer (HTTP %d), such as one with 100 threads or one too old to update", status), summary))
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity || status == http.StatusRequestEntityTooLarge ||
		status == http.StatusUnsupportedMediaType:
		outcome.failure = helpScoutFailure(operationID, sdkgo.FailureValidation, withErrorSummary(fmt.Sprintf("Help Scout rejected the request (HTTP %d)", status), summary))
	default:
		outcome.failure = helpScoutFailure(operationID, sdkgo.FailureProviderRejection, withErrorSummary(fmt.Sprintf("Help Scout rejected the request (HTTP %d)", status), summary))
	}
	return outcome
}

// describeHelpScoutError reads logRef, paths, and error codes; messages and rejected values are never read.
func describeHelpScoutError(body []byte, accessToken string) helpScoutErrorSummary {
	var document struct {
		LogRef   string `json:"logRef"`
		Embedded struct {
			Errors []struct {
				Path  string `json:"path"`
				Links struct {
					About struct {
						Href string `json:"href"`
					} `json:"about"`
				} `json:"_links"`
			} `json:"errors"`
		} `json:"_embedded"`
	}
	summary := helpScoutErrorSummary{}
	if json.Unmarshal(body, &document) != nil {
		return summary
	}
	if logRefPattern.MatchString(document.LogRef) && isCredentialFreeToken(document.LogRef, accessToken) {
		summary.logRef = document.LogRef
	}
	for _, providerError := range document.Embedded.Errors {
		if len(summary.fields) == maximumReportedErrorPaths {
			break
		}
		if !errorPathPattern.MatchString(providerError.Path) || !isCredentialFreeToken(providerError.Path, accessToken) {
			continue
		}
		field := providerError.Path
		_, code, hasCode := strings.Cut(providerError.Links.About.Href, "#")
		if hasCode && errorCodePattern.MatchString(code) && isCredentialFreeToken(code, accessToken) {
			field += "=" + code
		}
		summary.fields = append(summary.fields, field)
	}
	return summary
}

// isCredentialFreeToken rejects an error token that echoes the access token back.
func isCredentialFreeToken(token string, accessToken string) bool {
	return accessToken == "" || !strings.Contains(token, accessToken)
}

func withErrorSummary(message string, summary helpScoutErrorSummary) string {
	if len(summary.fields) == 0 {
		return message
	}
	return message + " [" + strings.Join(summary.fields, ", ") + "]"
}

// helpScoutRateLimitDelay prefers Help Scout's X-RateLimit-Retry-After seconds and then Retry-After.
func helpScoutRateLimitDelay(header http.Header, now time.Time) time.Duration {
	seconds, err := strconv.ParseInt(strings.TrimSpace(header.Get(rateLimitRetryAfterHeader)), 10, 64)
	if err == nil && seconds > 0 {
		return min(time.Duration(seconds)*time.Second, providerhttp.MaxRetryAfterDelay)
	}
	return providerhttp.ParseRetryAfter(header.Get("Retry-After"), now)
}

func (client *Client) receipt(call sdkgo.Call, providerObjectID int64, logRef string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: ConnectorID,
		ProviderRequestID: logRef, ObservedAt: client.now().UTC(),
	}
	if providerObjectID > 0 {
		receipt.ProviderObjectID = strconv.FormatInt(providerObjectID, 10)
	}
	return receipt
}

func conversationPath(conversationID int64) string {
	return "/v2/conversations/" + strconv.FormatInt(conversationID, 10)
}

func helpScoutFailure(operationID string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: ConnectorID, Operation: operationID, Message: message}
}

func helpScoutFailurePointer(operationID string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := helpScoutFailure(operationID, kind, message)
	return &failure
}

// responseFailureKind names a read or decode failure: an oversized body or a malformed one.
func responseFailureKind(err error) sdkgo.FailureKind {
	if errors.Is(err, errHelpScoutResponseTooLarge) {
		return sdkgo.FailureResponseTooLarge
	}
	return sdkgo.FailureProtocol
}

func responseFailureMessage(err error) string {
	if errors.Is(err, errHelpScoutResponseTooLarge) {
		return errHelpScoutResponseTooLarge.Error()
	}
	return errHelpScoutResponseMalformed.Error()
}
