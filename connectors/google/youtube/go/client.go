// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package youtube connects Dex applications to YouTube through the YouTube
// Data API v3 with a Google OAuth connection.
//
// A Client exposes five operations: SearchVideos searches public videos,
// GetVideo and GetChannel read one video or channel, ListPlaylistItems reads
// one page of a playlist such as a channel's uploads, and AddPlaylistItem adds
// a video to a playlist the authorized account owns.
//
// YouTube accepts no idempotency key, and a playlist listing can lag behind an
// insert. AddPlaylistItem lists the playlist's items for the video and inserts
// only when none exists. A single-dispatch fence then forbids a second insert
// after one that may have reached YouTube: the next attempt only lists the
// playlist and selects uncertain when the video is not shown.
//
// A Flow opens the connection with NewProjectConnection and adds a generated
// factory such as NewAddPlaylistItemStep; the runnable
// examples/playlist-curator Flow shows the complete registration.
package youtube

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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	providerName          = "youtube"
	defaultRequestTimeout = 25 * time.Second
	// MaximumPageSize is YouTube's largest maxResults for search, playlist items, and other lists.
	MaximumPageSize = 50
	// maximumPageTokenBytes bounds the opaque page token YouTube returns.
	maximumPageTokenBytes = 256
)

var (
	videoIDPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	channelIDPattern  = regexp.MustCompile(`^UC[A-Za-z0-9_-]{22}$`)
	playlistIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{2,64}$`)
	handlePattern     = regexp.MustCompile(`^@?[A-Za-z0-9._-]{3,30}$`)
	pageTokenPattern  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

	errRequestNotBuilt   = errors.New("YouTube request could not be built")
	errResponseTooLarge  = errors.New("YouTube response exceeds the configured size limit")
	errResponseMalformed = errors.New("YouTube returned a malformed response")

	// quotaReasons mean the project's daily quota is spent; waiting minutes cannot restore it.
	quotaReasons = []string{"quotaExceeded", "dailyLimitExceeded"}
	// rateLimitReasons are short-term limits that a later attempt can pass.
	rateLimitReasons = []string{"rateLimitExceeded", "userRateLimitExceeded"}
	// documentedReasons are the Google error reasons a Failure may name; any other reason is omitted.
	documentedReasons = []string{
		"quotaExceeded", "dailyLimitExceeded", "rateLimitExceeded", "userRateLimitExceeded", "forbidden",
		"insufficientPermissions", "authError", "invalidCredentials", "badRequest", "invalidParameter",
		"videoNotFound", "playlistNotFound", "channelNotFound", "playlistItemsNotAccessible",
		"playlistContainsMaximumNumberOfVideos", "manualSortRequired", "videoAlreadyInPlaylist",
		"backendError", "notFound", "youtubeSignupRequired",
	}
)

// Option supplies a non-serializable client dependency.
type Option interface{ applyClientOption(*clientOptions) }

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
}

type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(options *clientOptions) {
	options.httpClient = option.client
}

// WithHTTPClient replaces the HTTP client used for YouTube and token refresh
// calls. The Client uses a copy that never follows redirects and, when the
// supplied client has no Timeout, applies a 25-second request timeout. The
// caller keeps ownership of the client and its transport.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client executes authenticated YouTube Data API calls. It holds immutable
// configuration and is safe for concurrent use by several Steps.
type Client struct {
	baseURL          string
	httpClient       *http.Client
	credentials      CredentialSource
	refreshDriver    sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes int64
	now              func() time.Time
}

// providerRequest is one YouTube HTTP request below the API base URL.
type providerRequest struct {
	method string
	path   string
	query  url.Values
	body   []byte
	// requiresManage rejects a read-only connection before any provider call.
	requiresManage bool
	// writeFence sends a non-idempotent write once through Dispatch; nil sends directly.
	writeFence *singledispatch.Fence
}

type providerResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

// outcomeDisposition is the class of one unsuccessful exchange before an operation maps it to a branch.
type outcomeDisposition uint8

const (
	outcomeRetry outcomeDisposition = iota + 1
	outcomeNotFound
	outcomeQuotaExhausted
	outcomeRejected
	outcomeInvalidResponse
	outcomeDefect
	// outcomeRetryUnprocessed is a Retry after a fenced write that YouTube provably did not process.
	outcomeRetryUnprocessed
	// outcomeWriteUnconfirmed is a fenced write that may have taken effect without a usable response.
	outcomeWriteUnconfirmed
)

// providerOutcome is one unsuccessful exchange: a Retry or a terminal branch class.
type providerOutcome struct {
	disposition outcomeDisposition
	failure     sdkgo.Failure
	retryAfter  time.Duration
	receipt     sdkgo.Receipt
}

// operationBranches names the branch each outcome selects for one operation.
type operationBranches struct {
	notFound        sdkgo.BranchID
	quotaExhausted  sdkgo.BranchID
	rejected        sdkgo.BranchID
	invalidResponse sdkgo.BranchID
	defect          sdkgo.BranchID
}

// New validates configuration and constructs a YouTube client without network
// access. A blank Endpoint uses https://www.googleapis.com/youtube/v3, and a
// zero MaxResponseBytes uses 4 MiB. Credentials are resolved, and refreshed
// when they near expiry, before every provider call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("YouTube credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("YouTube endpoint: %w", err)
	}
	if config.MaxResponseBytes <= 0 {
		return nil, errors.New("YouTube maxResponseBytes must be positive")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("YouTube connector option is nil")
		}
		option.applyClientOption(&dependencies)
	}
	httpClient := providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout)
	return &Client{
		baseURL: baseURL, httpClient: httpClient, credentials: credentials,
		refreshDriver:    NewCredentialRefreshDriver(httpClient),
		maxResponseBytes: config.MaxResponseBytes, now: dependencies.now,
	}, nil
}

// SearchVideos returns the video search Query bound to this client.
func (client *Client) SearchVideos() SearchVideosOperation {
	return SearchVideosOperation{client: client}
}

// GetVideo returns the video read Query bound to this client.
func (client *Client) GetVideo() GetVideoOperation { return GetVideoOperation{client: client} }

// GetChannel returns the channel read Query bound to this client.
func (client *Client) GetChannel() GetChannelOperation { return GetChannelOperation{client: client} }

// ListPlaylistItems returns the playlist page Query bound to this client.
func (client *Client) ListPlaylistItems() ListPlaylistItemsOperation {
	return ListPlaylistItemsOperation{client: client}
}

// AddPlaylistItem returns the at-most-once playlist Mutation bound to this client.
func (client *Client) AddPlaylistItem() AddPlaylistItemOperation {
	return AddPlaylistItemOperation{client: client}
}

// exchange authenticates and sends one request, refreshing once after a 401, then classifies any non-2xx outcome.
func (client *Client) exchange(call sdkgo.Call, operation string, request providerRequest, objectID string) (providerResponse, *providerOutcome) {
	credential, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if outcome := client.credentialOutcome(operation, credential, err); outcome != nil {
		return providerResponse{}, outcome
	}
	if request.requiresManage && credential.AuthMethodID != ManageAuthMethodID {
		return providerResponse{}, client.outcome(outcomeRejected, operation, sdkgo.FailureAuthorization,
			"addPlaylistItem requires a YouTube connection authorized with the google-oauth-manage method and its youtube scope", client.receipt(call, objectID, nil))
	}
	if request.writeFence != nil {
		return client.exchangeFencedWrite(call, operation, request, objectID, credential)
	}
	response, credentialFailure, err := client.sendWithRefresh(call, call.Context, operation, credential, request, nil)
	if credentialFailure != nil {
		return providerResponse{}, credentialFailure
	}
	receipt := client.receipt(call, objectID, response.header)
	switch {
	case errors.Is(err, errRequestNotBuilt):
		return response, client.outcome(outcomeDefect, operation, sdkgo.FailureLocalDefect, errRequestNotBuilt.Error(), receipt)
	case errors.Is(err, errResponseTooLarge):
		return response, client.outcome(outcomeInvalidResponse, operation, sdkgo.FailureResponseTooLarge, errResponseTooLarge.Error(), receipt)
	case err != nil:
		return response, client.outcome(outcomeRetry, operation, sdkgo.FailureTransport, "YouTube did not return a complete response", receipt)
	case response.statusCode >= 200 && response.statusCode < 300:
		return response, nil
	}
	classified := classifyErrorResponse(operation, response, client.now())
	classified.receipt = receipt
	return response, &classified
}

// exchangeFencedWrite sends a write once through its fence. Only a refusal that proves
// YouTube did not act, such as 429, 503, or a connection that never opened, is an
// unprocessed Retry; any other failure after the claim may have applied the write.
func (client *Client) exchangeFencedWrite(call sdkgo.Call, operation string, request providerRequest, objectID string, credential Credentials) (providerResponse, *providerOutcome) {
	observation := &writeDispatchObservation{}
	var response providerResponse
	var credentialFailure *providerOutcome
	var sendErr error
	dispatchErr := request.writeFence.Dispatch(func(requestContext context.Context) error {
		response, credentialFailure, sendErr = client.sendWithRefresh(call, requestContext, operation, credential, request, observation)
		return sendErr
	})
	receipt := client.receipt(call, objectID, response.header)
	switch {
	case errors.Is(dispatchErr, singledispatch.ErrCheckpoint), errors.Is(dispatchErr, singledispatch.ErrInsufficientTime), errors.Is(dispatchErr, singledispatch.ErrDispatchNotAllowed):
		return providerResponse{}, client.outcome(outcomeRetry, operation, sdkgo.FailureTransport, "the YouTube insert was not started", receipt)
	case credentialFailure != nil:
		// A credential failure follows a 401, which YouTube returns without inserting.
		return providerResponse{}, credentialFailure
	case errors.Is(sendErr, errRequestNotBuilt):
		return response, client.outcome(outcomeDefect, operation, sdkgo.FailureLocalDefect, errRequestNotBuilt.Error(), receipt)
	case errors.Is(sendErr, errResponseTooLarge):
		return response, client.outcome(outcomeWriteUnconfirmed, operation, sdkgo.FailureResponseTooLarge, errResponseTooLarge.Error(), receipt)
	case sendErr != nil && observation.isProvablyUndispatched():
		return response, client.outcome(outcomeRetryUnprocessed, operation, sdkgo.FailureTransport, "YouTube could not be reached before sending", receipt)
	case sendErr != nil:
		return response, client.outcome(outcomeWriteUnconfirmed, operation, sdkgo.FailureTransport, "YouTube did not return a complete response", receipt)
	case response.statusCode >= 200 && response.statusCode < 300:
		return response, nil
	}
	classified := classifyErrorResponse(operation, response, client.now())
	classified.receipt = receipt
	if classified.disposition == outcomeRetry {
		classified.disposition = outcomeWriteUnconfirmed
		if classified.failure.Kind == sdkgo.FailureRateLimit || response.statusCode == http.StatusServiceUnavailable {
			classified.disposition = outcomeRetryUnprocessed
		}
	}
	return response, &classified
}

// sendWithRefresh sends a request and, after a 401 from a refreshing source, refreshes once and resends.
func (client *Client) sendWithRefresh(call sdkgo.Call, requestContext context.Context, operation string, credential Credentials, request providerRequest, observation *writeDispatchObservation) (providerResponse, *providerOutcome, error) {
	response, err := client.send(requestContext, credential, request, observation)
	if err == nil && response.statusCode == http.StatusUnauthorized {
		if _, ok := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); ok {
			refreshed, refreshErr := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
			if outcome := client.credentialOutcome(operation, refreshed, refreshErr); outcome != nil {
				return providerResponse{}, outcome, nil
			}
			response, err = client.send(requestContext, refreshed, request, observation)
		}
	}
	return response, nil, err
}

func (client *Client) send(requestContext context.Context, credential Credentials, request providerRequest, observation *writeDispatchObservation) (providerResponse, error) {
	var body io.Reader
	if request.body != nil {
		body = bytes.NewReader(request.body)
	}
	target := client.baseURL + request.path
	if len(request.query) > 0 {
		target += "?" + request.query.Encode()
	}
	if observation != nil {
		observation.reset()
		requestContext = httptrace.WithClientTrace(requestContext, observation.clientTrace())
	}
	httpRequest, err := http.NewRequestWithContext(requestContext, request.method, target, body)
	if err != nil {
		return providerResponse{}, errRequestNotBuilt
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
	if request.body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	if request.writeFence != nil {
		if err := singledispatch.PrepareHTTPWrite(httpRequest); err != nil {
			return providerResponse{}, errRequestNotBuilt
		}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return providerResponse{}, err
	}
	// Closing the body cannot change an outcome already read.
	defer func() { _ = httpResponse.Body.Close() }()
	response := providerResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	limit := client.maxResponseBytes
	if !isSuccess {
		limit = providerhttp.MaxErrorBodyBytes
	}
	response.body, err = providerhttp.ReadBoundedBody(httpResponse.Body, limit)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		if !isSuccess {
			// An oversized error body still classifies by status alone.
			return response, nil
		}
		return response, errResponseTooLarge
	}
	return response, err
}

// writeDispatchObservation proves a write unsent only when connecting failed and no connection was obtained.
type writeDispatchObservation struct {
	mutex                 sync.Mutex
	hasFailedToConnect    bool
	hasObtainedConnection bool
}

func (observation *writeDispatchObservation) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSDone: observation.recordDNSDone, ConnectDone: observation.recordConnectDone,
		TLSHandshakeDone: observation.recordTLSHandshakeDone, GotConn: observation.recordObtainedConnection,
	}
}

func (observation *writeDispatchObservation) isProvablyUndispatched() bool {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	return observation.hasFailedToConnect && !observation.hasObtainedConnection
}

// reset forgets a previous send, such as the 401 before a refreshed resend.
func (observation *writeDispatchObservation) reset() {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasFailedToConnect, observation.hasObtainedConnection = false, false
}

func (observation *writeDispatchObservation) recordDNSDone(info httptrace.DNSDoneInfo) {
	if info.Err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *writeDispatchObservation) recordConnectDone(_ string, _ string, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *writeDispatchObservation) recordTLSHandshakeDone(_ tls.ConnectionState, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *writeDispatchObservation) recordObtainedConnection(httptrace.GotConnInfo) {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasObtainedConnection = true
}

func (observation *writeDispatchObservation) recordConnectionFailure() {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasFailedToConnect = true
}

// credentialOutcome separates a revoked grant from a missing or malformed access token.
func (client *Client) credentialOutcome(operation string, credential Credentials, err error) *providerOutcome {
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return client.outcome(outcomeRejected, operation, sdkgo.FailureAuthentication, "the YouTube connection must be authorized again", sdkgo.Receipt{})
	case err != nil, !providerhttp.IsHeaderSafeCredential(credential.AccessToken.Reveal()):
		return client.outcome(outcomeDefect, operation, sdkgo.FailureAuthentication, "YouTube connection credentials are unavailable or invalid", sdkgo.Receipt{})
	}
	return nil
}

func (client *Client) outcome(disposition outcomeDisposition, operation string, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) *providerOutcome {
	return &providerOutcome{disposition: disposition, failure: providerFailure(operation, kind, message), receipt: receipt}
}

func (client *Client) receipt(call sdkgo.Call, objectID string, header http.Header) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
	if header != nil {
		if requestID := header.Get("X-Goog-Request-Id"); requestID != "" && len(requestID) <= 128 && pageTokenPattern.MatchString(requestID) {
			receipt.ProviderRequestID = requestID
		}
	}
	return receipt
}

// classifyErrorResponse maps a non-2xx status and Google's error reason without reading message text.
func classifyErrorResponse(operation string, response providerResponse, now time.Time) providerOutcome {
	reason := readErrorReason(response.body)
	detail := ""
	if reason != "" {
		detail = ", " + reason
	}
	status := response.statusCode
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), now)
	switch {
	case slices.Contains(quotaReasons, reason):
		return terminalOutcome(outcomeQuotaExhausted, operation, sdkgo.FailureQuotaExhausted, fmt.Sprintf("the project's daily YouTube Data API quota is spent (HTTP %d%s)", status, detail))
	case status == http.StatusTooManyRequests || slices.Contains(rateLimitReasons, reason):
		return providerOutcome{
			disposition: outcomeRetry, retryAfter: retryAfter,
			failure: providerFailure(operation, sdkgo.FailureRateLimit, fmt.Sprintf("YouTube rate limited the request (HTTP %d%s)", status, detail)),
		}
	case status == http.StatusRequestTimeout || (status >= 500 && status != http.StatusNotImplemented):
		return providerOutcome{
			disposition: outcomeRetry, retryAfter: retryAfter,
			failure: providerFailure(operation, sdkgo.FailureAvailability, fmt.Sprintf("YouTube is temporarily unavailable (HTTP %d%s)", status, detail)),
		}
	case status == http.StatusUnauthorized:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureAuthentication, fmt.Sprintf("Google rejected the access token after one refresh (HTTP 401%s)", detail))
	case status == http.StatusNotFound:
		return terminalOutcome(outcomeNotFound, operation, sdkgo.FailureNotFound, fmt.Sprintf("YouTube found no such video, channel, or playlist (HTTP 404%s)", detail))
	case status == http.StatusForbidden:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureAuthorization, fmt.Sprintf("YouTube denied access: check the youtube scope and that the account owns the playlist (HTTP 403%s)", detail))
	case status == http.StatusBadRequest || status == http.StatusConflict:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureValidation, fmt.Sprintf("YouTube rejected the request as invalid (HTTP %d%s)", status, detail))
	case status >= 300 && status < 400:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureProtocol, fmt.Sprintf("YouTube redirected the request (HTTP %d)", status))
	default:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureProviderRejection, fmt.Sprintf("YouTube rejected the request (HTTP %d%s)", status, detail))
	}
}

// readErrorReason returns Google's documented error reason, never its message.
func readErrorReason(body []byte) string {
	for _, token := range providerhttp.ReadErrorTokens(body, []string{"/error/errors/0/reason"}) {
		if slices.Contains(documentedReasons, token) {
			return token
		}
	}
	return ""
}

func terminalOutcome(disposition outcomeDisposition, operation string, kind sdkgo.FailureKind, message string) providerOutcome {
	return providerOutcome{disposition: disposition, failure: providerFailure(operation, kind, message)}
}

func providerFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func providerFailurePointer(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := providerFailure(operation, kind, message)
	return &failure
}

// decodeResponse decodes a complete 2xx body into destination.
func decodeResponse(body []byte, destination any) error {
	if err := json.Unmarshal(body, destination); err != nil {
		return errResponseMalformed
	}
	return nil
}

// validatePageRequest checks a page size and token and sets them on query.
func validatePageRequest(query url.Values, maxResults int, pageToken string) error {
	if maxResults < 0 || maxResults > MaximumPageSize {
		return fmt.Errorf("maxResults must be from 1 through %d, or zero for YouTube's default of 5", MaximumPageSize)
	}
	if maxResults > 0 {
		query.Set("maxResults", strconv.Itoa(maxResults))
	}
	if pageToken != "" {
		if len(pageToken) > maximumPageTokenBytes || !pageTokenPattern.MatchString(pageToken) {
			return errors.New("pageToken must be a nextPageToken returned by YouTube")
		}
		query.Set("pageToken", pageToken)
	}
	return nil
}

// validateReturnedPageToken rejects a next page token that a later call could not send back.
func validateReturnedPageToken(token string) error {
	if token != "" && (len(token) > maximumPageTokenBytes || !pageTokenPattern.MatchString(token)) {
		return errors.New("YouTube returned an invalid nextPageToken")
	}
	return nil
}

// parseOptionalTime parses an RFC 3339 provider time, returning nil for a blank value.
func parseOptionalTime(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, errors.New("YouTube returned an invalid timestamp")
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

// parseOptionalCount parses YouTube's decimal-string statistic, returning nil when YouTube hides it.
func parseOptionalCount(value *string) (*int64, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(*value, 10, 64)
	if err != nil || parsed < 0 {
		return nil, errors.New("YouTube returned an invalid statistic")
	}
	return &parsed, nil
}

// queryAttemptForOutcome maps an unsuccessful exchange to the Query's Retry or branch.
func queryAttemptForOutcome[T any](outcome providerOutcome, branches operationBranches) sdkgo.QueryAttempt[T] {
	var zero T
	if outcome.disposition == outcomeRetry {
		return sdkgo.NewQueryRetry[T](outcome.failure, outcome.retryAfter)
	}
	return sdkgo.NewQueryBranch(branches.branchFor(outcome.disposition), zero, &outcome.failure, outcome.receipt)
}

// mutationAttemptForOutcome maps an unsuccessful exchange that changed nothing to Retry or a branch.
func mutationAttemptForOutcome[T any](outcome providerOutcome, branches operationBranches) sdkgo.MutationAttempt[T] {
	var zero T
	if outcome.disposition == outcomeRetry {
		return sdkgo.NewMutationRetry[T](outcome.failure, outcome.retryAfter)
	}
	return sdkgo.NewMutationBranch(branches.branchFor(outcome.disposition), zero, &outcome.failure, outcome.receipt)
}

func (branches operationBranches) branchFor(disposition outcomeDisposition) sdkgo.BranchID {
	switch disposition {
	case outcomeNotFound:
		if branches.notFound == "" {
			return branches.rejected
		}
		return branches.notFound
	case outcomeQuotaExhausted:
		return branches.quotaExhausted
	case outcomeRejected:
		return branches.rejected
	case outcomeInvalidResponse:
		return branches.invalidResponse
	default:
		return branches.defect
	}
}
