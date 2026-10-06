// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package front connects Dex applications to the Front Core API.
//
// Queries search conversations, read one conversation with its newest messages and internal comments,
// and find a contact by email. Mutations send a reply or add an internal comment once per Step execution,
// guarded by a Dex checkpoint that narrows but does not close the duplicate window after a lost Worker,
// and set a conversation's status or ticket status, assignee, and tags, safely repeatable.
//
// A connection holds one Front API token, sent as a bearer token only to https://api2.frontapp.com. The
// connector takes and returns Front's own conversation statuses, ticket status IDs, and teammate, inbox,
// and tag IDs and never maps them to another vocabulary. The runnable examples/conversation-triage
// application uses every operation and the inbox, teammate, and tag pickers.
package front

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// frontAPIBaseURL is the only host the connector sends the API token to.
	frontAPIBaseURL = "https://api2.frontapp.com"
	// frontRequestTimeout bounds one request; each Step's Execute timeout bounds the whole operation.
	frontRequestTimeout = 25 * time.Second
	providerName        = "front"
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	apiBaseURL string
	now        func() time.Time
}

// WithHTTPClient overrides the HTTP client, whose default timeout is 25 seconds per request. The caller
// keeps ownership of the client and its transport. The connector uses a copy that never follows a
// redirect, so the API token is never replayed to another host.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAPIBaseURL replaces https://api2.frontapp.com for a local Front-compatible fake. The URL must use
// HTTPS unless its host is loopback, and it must not carry user information, a query, or a fragment.
// Production connections leave it unset.
func WithAPIBaseURL(baseURL string) Option {
	return func(options *clientOptions) { options.apiBaseURL = baseURL }
}

// WithClock replaces the clock used for receipts and rate-limit waits. Tests use it; production
// connections leave it unset.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// Client executes authenticated Front Core API requests for one connection configuration. It is safe for
// concurrent use by several Steps.
type Client struct {
	apiBaseURL       string
	httpClient       *http.Client
	credentials      CredentialSource
	maxResponseBytes int64
	now              func() time.Time
}

// New validates config and constructs a Client. Blank configuration fields take their manifest defaults.
// Credentials are resolved before every provider request, so a replaced API token takes effect without a
// restart; the response limit is startup configuration.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Front maxResponseBytes must be positive")
	}
	if credentials == nil {
		return nil, errors.New("Front credential provider is required")
	}
	dependencies := clientOptions{now: time.Now, apiBaseURL: frontAPIBaseURL}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Front connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("Front connector clock is required")
	}
	apiBaseURL, err := providerhttp.ValidateBaseURL(dependencies.apiBaseURL)
	if err != nil {
		return nil, fmt.Errorf("Front API base URL: %w", err)
	}
	return &Client{
		apiBaseURL: apiBaseURL, credentials: credentials,
		httpClient:       providerhttp.NewProviderHTTPClient(dependencies.httpClient, frontRequestTimeout),
		maxResponseBytes: config.MaxResponseBytes, now: dependencies.now,
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

// FindContactByEmail returns the findContactByEmail Query bound to this client.
func (client *Client) FindContactByEmail() FindContactByEmailOperation {
	return FindContactByEmailOperation{client: client}
}

// resolveCredentials returns a header-safe API token, or a safe defect Failure.
func (client *Client) resolveCredentials(call sdkgo.Call, operationID string) (Credentials, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIToken.Reveal()) {
		return Credentials{}, frontFailurePointer(sdkgo.FailureAuthentication, operationID,
			"the Front API token is unavailable, blank, or contains characters a header cannot carry")
	}
	return credentials, nil
}

// exchange sends one authenticated request within requestContext and classifies the answer.
func (client *Client) exchange(
	requestContext context.Context, credentials Credentials, operationID string, request frontRequest,
) frontExchange {
	if requestContext == nil {
		requestContext = context.Background()
	}
	if requestContext.Err() != nil {
		return frontExchange{outcome: exchangeNotSent, failure: frontFailure(sdkgo.FailureTransport, operationID, "the request deadline passed before Front was contacted; no request was sent")}
	}
	observation := &dispatchObservation{}
	httpRequest, err := client.buildRequest(requestContext, credentials, request, observation)
	if err != nil {
		return frontExchange{outcome: exchangeDefect, failure: frontFailure(sdkgo.FailureLocalDefect, operationID, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if observation.isProvablyUndispatched() || isConnectionNeverEstablished(err) {
			return frontExchange{outcome: exchangeNotSent, failure: frontFailure(sdkgo.FailureTransport, operationID, "Front could not be reached; no request was sent")}
		}
		return frontExchange{outcome: exchangeUnconfirmed, failure: frontFailure(sdkgo.FailureTransport, operationID, "the Front request may have been sent, but no answer arrived")}
	}
	response := frontResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	limit := client.maxResponseBytes
	if !isSuccess {
		limit = providerhttp.MaxErrorBodyBytes
	}
	body, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, limit)
	closeErr := httpResponse.Body.Close()
	switch {
	case isSuccess && errors.Is(readErr, providerhttp.ErrBodyTooLarge):
		return frontExchange{outcome: exchangeInvalid, response: response,
			failure: frontFailure(sdkgo.FailureResponseTooLarge, operationID, "the Front response exceeds the configured maxResponseBytes")}
	case isSuccess && (readErr != nil || closeErr != nil):
		return frontExchange{outcome: exchangeUnconfirmed, response: response,
			failure: frontFailure(sdkgo.FailureTransport, operationID, "the Front response could not be read")}
	case isSuccess && bytes.Contains(body, []byte(credentials.APIToken.Reveal())):
		return frontExchange{outcome: exchangeInvalid, response: response,
			failure: frontFailure(sdkgo.FailureProtocol, operationID, "the Front response reflected the API token")}
	case isSuccess:
		response.body = body
		return frontExchange{outcome: exchangeSucceeded, response: response}
	}
	// Only the status decides a failure, so an unreadable error body changes nothing.
	return client.classifyFailureStatus(response, operationID)
}

func (client *Client) buildRequest(
	requestContext context.Context, credentials Credentials, request frontRequest, observation *dispatchObservation,
) (*http.Request, error) {
	var body io.Reader
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return nil, errFrontRequestNotBuilt
		}
		body = bytes.NewReader(encoded)
	}
	target := client.apiBaseURL + request.path
	if len(request.query) != 0 {
		target += "?" + request.query.Encode()
	}
	tracedContext := httptrace.WithClientTrace(requestContext, observation.clientTrace())
	httpRequest, err := http.NewRequestWithContext(tracedContext, request.method, target, body)
	if err != nil {
		return nil, errFrontRequestNotBuilt
	}
	httpRequest.Header.Set("Authorization", "Bearer "+credentials.APIToken.Reveal())
	httpRequest.Header.Set("Accept", "application/json")
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	return httpRequest, nil
}

// classifyFailureStatus maps a non-2xx answer by its status alone; Front's _error title and message are never read.
func (client *Client) classifyFailureStatus(response frontResponse, operationID string) frontExchange {
	status := response.statusCode
	result := frontExchange{response: response}
	switch {
	case status == http.StatusTooManyRequests:
		result.outcome, result.retryAfter = exchangeRateLimited, client.rateLimitDelay(response.header)
		result.failure = frontFailure(sdkgo.FailureRateLimit, operationID, "Front rate limited the request (HTTP 429)")
	case status == http.StatusRequestTimeout || status >= 500:
		result.outcome = exchangeUnconfirmed
		result.failure = frontFailure(sdkgo.FailureAvailability, operationID, fmt.Sprintf("Front could not complete the request (HTTP %d)", status))
	case status == http.StatusMovedPermanently:
		result.outcome = exchangeMerged
		result.failure = frontFailure(sdkgo.FailureNotFound, operationID, "Front reports that the conversation was merged into another (HTTP 301)")
	case status == http.StatusNotFound || status == http.StatusGone:
		result.outcome = exchangeNotFound
		result.failure = frontFailure(sdkgo.FailureNotFound, operationID, fmt.Sprintf("Front found no such resource (HTTP %d)", status))
	case status >= 300 && status < 400:
		result.outcome = exchangeRejected
		result.failure = frontFailure(sdkgo.FailureProtocol, operationID, fmt.Sprintf("Front redirected the request (HTTP %d), which the connector never follows", status))
	case status == http.StatusUnauthorized:
		result.outcome = exchangeRejected
		result.failure = frontFailure(sdkgo.FailureAuthentication, operationID, "Front rejected the API token (HTTP 401); check that the token still exists")
	case status == http.StatusForbidden:
		result.outcome = exchangeRejected
		result.failure = frontFailure(sdkgo.FailureAuthorization, operationID,
			"Front denied access (HTTP 403); check the token's namespaces and its read, write, or send permission for this resource")
	default:
		result.outcome = exchangeRejected
		result.failure = frontFailure(rejectionFailureKind(status), operationID, fmt.Sprintf("Front rejected the request (HTTP %d)", status))
	}
	return result
}

// rateLimitDelay honors Retry-After seconds, then the X-Ratelimit-Reset Unix second, which is at most a minute out.
func (client *Client) rateLimitDelay(header http.Header) time.Duration {
	now := client.now()
	if delay := providerhttp.ParseRetryAfter(header.Get("Retry-After"), now); delay > 0 {
		return delay
	}
	reset, err := strconv.ParseInt(strings.TrimSpace(header.Get(rateLimitResetHeader)), 10, 64)
	if err != nil || reset <= 0 {
		return 0
	}
	return min(max(time.Unix(reset, 0).Sub(now), time.Second), time.Minute)
}

// nextPageToken takes page_token from a next link on Front's API hosts; the link itself is never followed.
func (client *Client) nextPageToken(next *string, expectedPathPrefix string) (string, error) {
	if next == nil || *next == "" {
		return "", nil
	}
	parsed, err := url.Parse(*next)
	if err != nil || parsed.User != nil || parsed.Fragment != "" {
		return "", errors.New("the next-page link is not a plain URL")
	}
	base, _ := url.Parse(client.apiBaseURL) // New validated the base URL.
	isFrontHost := parsed.Scheme == "https" && (parsed.Host == "api2.frontapp.com" || companyAPIHostPattern.MatchString(parsed.Host))
	if !isFrontHost && (parsed.Scheme != base.Scheme || parsed.Host != base.Host) {
		return "", errors.New("the next-page link points outside Front's API hosts")
	}
	if !strings.HasPrefix(parsed.EscapedPath(), expectedPathPrefix) {
		return "", errors.New("the next-page link names another resource")
	}
	token := parsed.Query().Get("page_token")
	if !pageTokenPattern.MatchString(token) {
		return "", errors.New("the next-page link has no valid page_token")
	}
	return token, nil
}

func (client *Client) receipt(call sdkgo.Call, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
}

func frontFailure(kind sdkgo.FailureKind, operationID string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operationID, Message: message}
}

func frontFailurePointer(kind sdkgo.FailureKind, operationID string, message string) *sdkgo.Failure {
	failure := frontFailure(kind, operationID, message)
	return &failure
}
