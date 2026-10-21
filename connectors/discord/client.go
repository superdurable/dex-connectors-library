// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package discord connects Dex applications to Discord channels through the
// Discord HTTP API with a bot token.
//
// A Client exposes four operations: GetChannel reads one channel,
// ListChannelMessages reads one bounded page of channel messages,
// CreateMessage sends a bot text message, and AddReaction adds the bot's
// reaction to a message.
//
// CreateMessage sends a nonce derived from the Step's idempotency key with
// enforce_nonce, so Discord returns the message an earlier attempt created
// instead of posting a duplicate. Discord enforces a nonce for only a few
// minutes, so the operation's retry window is two minutes. AddReaction is
// repeat-safe because Discord keeps one reaction per user and emoji.
//
// A Flow opens the connection with NewProjectConnection and adds a generated
// factory such as NewCreateMessageStep; the runnable examples/channel-announcement
// Flow shows the complete registration.
package discord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName          = "discord"
	defaultRequestTimeout = 25 * time.Second
	// userAgent follows Discord's required DiscordBot (url, version) form.
	userAgent = "DiscordBot (https://github.com/superdurable/dex-connectors-library, 0.1.0)"
)

var (
	// snowflakePattern matches a Discord snowflake ID in decimal form.
	snowflakePattern     = regexp.MustCompile(`^[0-9]{17,20}$`)
	errRequestNotBuilt   = errors.New("Discord request could not be built")
	errResponseTooLarge  = errors.New("Discord response exceeds the configured size limit")
	errResponseMalformed = errors.New("Discord returned a malformed response")
	// documentedErrorCodes are the JSON error codes a Failure may name; any other code is omitted.
	documentedErrorCodes = []string{
		"0", "10003", "10008", "10014", "20028", "30010", "40001", "40005", "50001", "50006", "50008", "50013", "50035", "50083", "90001",
	}
	// notFoundErrorCodes are Unknown Channel, Unknown Message, and Unknown Emoji.
	notFoundErrorCodes = []string{"10003", "10008", "10014"}
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

// WithHTTPClient replaces the HTTP client used for Discord API calls. The
// Client uses a copy that never follows redirects and, when the supplied
// client has no Timeout, applies a 25-second request timeout. The caller keeps
// ownership of the client and its transport.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client executes authenticated Discord HTTP API calls. It holds immutable
// configuration and is safe for concurrent use by several Steps.
type Client struct {
	baseURL          string
	httpClient       *http.Client
	credentials      CredentialSource
	maxResponseBytes int64
	now              func() time.Time
}

// providerRequest is one Discord HTTP request below the API base URL.
type providerRequest struct {
	method string
	path   string
	body   []byte
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
	outcomeRejected
	outcomeInvalidResponse
	outcomeDefect
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
	rejected        sdkgo.BranchID
	invalidResponse sdkgo.BranchID
	defect          sdkgo.BranchID
}

// New validates configuration and constructs a Discord client without network
// access. A blank Endpoint uses https://discord.com/api/v10, and a zero
// MaxResponseBytes uses 4 MiB. The credential source is consulted before every
// provider call, so a replaced bot token takes effect without a restart.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, errors.New("Discord credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Discord endpoint: %w", err)
	}
	if config.MaxResponseBytes <= 0 {
		return nil, errors.New("Discord maxResponseBytes must be positive")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Discord connector option is nil")
		}
		option.applyClientOption(&dependencies)
	}
	return &Client{
		baseURL: baseURL, httpClient: providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials: credentials, maxResponseBytes: config.MaxResponseBytes, now: dependencies.now,
	}, nil
}

// GetChannel returns the channel read Query bound to this client.
func (client *Client) GetChannel() GetChannelOperation { return GetChannelOperation{client: client} }

// ListChannelMessages returns the message page Query bound to this client.
func (client *Client) ListChannelMessages() ListChannelMessagesOperation {
	return ListChannelMessagesOperation{client: client}
}

// CreateMessage returns the message send Mutation bound to this client.
func (client *Client) CreateMessage() CreateMessageOperation {
	return CreateMessageOperation{client: client}
}

// AddReaction returns the reaction Mutation bound to this client.
func (client *Client) AddReaction() AddReactionOperation { return AddReactionOperation{client: client} }

// exchange authenticates and sends one request, then classifies any non-2xx outcome.
func (client *Client) exchange(call sdkgo.Call, operation string, request providerRequest, objectID string) (providerResponse, *providerOutcome) {
	credentials, outcome := client.resolveCredentials(call, operation)
	if outcome != nil {
		return providerResponse{}, outcome
	}
	response, err := client.send(call, credentials, request)
	receipt := client.receipt(call, objectID)
	switch {
	case errors.Is(err, errRequestNotBuilt):
		return response, client.outcome(outcomeDefect, operation, sdkgo.FailureLocalDefect, errRequestNotBuilt.Error(), receipt)
	case errors.Is(err, errResponseTooLarge):
		return response, client.outcome(outcomeInvalidResponse, operation, sdkgo.FailureResponseTooLarge, errResponseTooLarge.Error(), receipt)
	case err != nil:
		return response, client.outcome(outcomeRetry, operation, sdkgo.FailureTransport, "Discord did not return a complete response", receipt)
	case response.statusCode >= 200 && response.statusCode < 300:
		return response, nil
	}
	classified := classifyErrorResponse(operation, response, client.now())
	classified.receipt = receipt
	return response, &classified
}

func (client *Client) send(call sdkgo.Call, credentials Credentials, request providerRequest) (providerResponse, error) {
	var body io.Reader
	if request.body != nil {
		body = bytes.NewReader(request.body)
	}
	httpRequest, err := http.NewRequestWithContext(call.Context, request.method, client.baseURL+request.path, body)
	if err != nil {
		return providerResponse{}, errRequestNotBuilt
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Authorization", "Bot "+credentials.BotToken.Reveal())
	httpRequest.Header.Set("User-Agent", userAgent)
	if request.body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return providerResponse{}, err
	}
	// Closing the body cannot change an outcome already read.
	defer func() { _ = httpResponse.Body.Close() }()
	response := providerResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header.Clone()}
	limit := client.maxResponseBytes
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		limit = providerhttp.MaxErrorBodyBytes
	}
	response.body, err = providerhttp.ReadBoundedBody(httpResponse.Body, limit)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		if limit == providerhttp.MaxErrorBodyBytes {
			// An oversized error body still classifies by status alone.
			return response, nil
		}
		return response, errResponseTooLarge
	}
	return response, err
}

// resolveCredentials separates a revoked grant from a missing or malformed token.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *providerOutcome) {
	credentials, err := client.credentials.Resolve(call)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return Credentials{}, client.outcome(outcomeRejected, operation, sdkgo.FailureAuthentication, "the Discord connection must be authorized again", sdkgo.Receipt{})
	case err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.BotToken.Reveal()):
		return Credentials{}, client.outcome(outcomeDefect, operation, sdkgo.FailureAuthentication, "Discord connection credentials are unavailable or invalid", sdkgo.Receipt{})
	}
	return credentials, nil
}

func (client *Client) outcome(disposition outcomeDisposition, operation string, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt) *providerOutcome {
	return &providerOutcome{disposition: disposition, failure: providerFailure(operation, kind, message), receipt: receipt}
}

func (client *Client) receipt(call sdkgo.Call, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
}

// classifyErrorResponse maps a non-2xx status and Discord's numeric error code without reading message text.
func classifyErrorResponse(operation string, response providerResponse, now time.Time) providerOutcome {
	code := readErrorCode(response.body)
	detail := ""
	if code != "" {
		detail = ", code " + code
	}
	status := response.statusCode
	switch {
	case status == http.StatusTooManyRequests:
		return providerOutcome{
			disposition: outcomeRetry, retryAfter: rateLimitDelay(response, now),
			failure: providerFailure(operation, sdkgo.FailureRateLimit, "Discord rate limited the request"),
		}
	case status == http.StatusRequestTimeout || (status >= 500 && status != http.StatusNotImplemented):
		return providerOutcome{
			disposition: outcomeRetry, retryAfter: providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), now),
			failure: providerFailure(operation, sdkgo.FailureAvailability, fmt.Sprintf("Discord is temporarily unavailable (HTTP %d%s)", status, detail)),
		}
	case status == http.StatusUnauthorized:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureAuthentication, fmt.Sprintf("Discord rejected the bot token (HTTP 401%s)", detail))
	case status == http.StatusForbidden:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureAuthorization, fmt.Sprintf("Discord denied access: check that the bot is in the server and has the channel permissions (HTTP 403%s)", detail))
	case status == http.StatusNotFound && (code == "" || slices.Contains(notFoundErrorCodes, code)):
		return terminalOutcome(outcomeNotFound, operation, sdkgo.FailureNotFound, fmt.Sprintf("Discord found no such channel, message, or emoji (HTTP 404%s)", detail))
	case status == http.StatusBadRequest || status == http.StatusRequestEntityTooLarge:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureValidation, fmt.Sprintf("Discord rejected the request as invalid (HTTP %d%s)", status, detail))
	case status >= 300 && status < 400:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureProtocol, fmt.Sprintf("Discord redirected the request (HTTP %d)", status))
	default:
		return terminalOutcome(outcomeRejected, operation, sdkgo.FailureProviderRejection, fmt.Sprintf("Discord rejected the request (HTTP %d%s)", status, detail))
	}
}

// rateLimitDelay prefers the body's fractional retry_after seconds over the whole-second Retry-After header.
func rateLimitDelay(response providerResponse, now time.Time) time.Duration {
	var body struct {
		RetryAfter *float64 `json:"retry_after"`
	}
	if json.Unmarshal(response.body, &body) == nil && body.RetryAfter != nil && *body.RetryAfter > 0 && !math.IsInf(*body.RetryAfter, 0) {
		seconds := min(*body.RetryAfter, providerhttp.MaxRetryAfterDelay.Seconds())
		return time.Duration(math.Ceil(seconds*1000)) * time.Millisecond
	}
	return providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), now)
}

// readErrorCode returns Discord's documented numeric JSON error code, never its message.
func readErrorCode(body []byte) string {
	tokens := providerhttp.ReadErrorTokens(body, []string{"/code"})
	if len(tokens) != 1 {
		return ""
	}
	if _, err := strconv.Atoi(tokens[0]); err != nil || !slices.Contains(documentedErrorCodes, tokens[0]) {
		return ""
	}
	return tokens[0]
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

// validateSnowflake checks that value is a Discord snowflake ID, so it is safe as one path segment.
func validateSnowflake(name string, value string) error {
	if !snowflakePattern.MatchString(value) {
		return fmt.Errorf("%s must be a Discord snowflake ID of 17 to 20 digits", name)
	}
	return nil
}

// queryAttemptForOutcome maps an unsuccessful exchange to the Query's Retry or branch.
func queryAttemptForOutcome[T any](outcome providerOutcome, branches operationBranches) sdkgo.QueryAttempt[T] {
	var zero T
	if outcome.disposition == outcomeRetry {
		return sdkgo.NewQueryRetry[T](outcome.failure, outcome.retryAfter)
	}
	return sdkgo.NewQueryBranch(branches.branchFor(outcome.disposition), zero, &outcome.failure, outcome.receipt)
}

// mutationAttemptForOutcome maps an unsuccessful exchange to Retry or a branch; both writes are repeat-safe.
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
		return branches.notFound
	case outcomeRejected:
		return branches.rejected
	case outcomeInvalidResponse:
		if branches.invalidResponse == "" {
			return branches.rejected
		}
		return branches.invalidResponse
	default:
		return branches.defect
	}
}
