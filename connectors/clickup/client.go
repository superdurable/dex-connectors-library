// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package clickup implements ClickUp task operations and a signed task webhook Trigger as Dex connector
// Steps. searchTasks, getTask, and findMemberByEmail read; createTask and addComment record a Dex
// heartbeat checkpoint before they write and reconcile an unconfirmed attempt by reading back instead of
// resending; updateTask and updateTaskTags apply absolute values and set memberships, so they are safe
// to repeat. Dex accepts the checkpoint when the Worker writes it to its stream, so a Worker lost before
// Dex stored it could still send a create or comment twice.
//
// The connector authenticates with a ClickUp personal API token sent as the raw Authorization header,
// sends every request to ClickUp API v2 at APIBaseURL, and passes ClickUp's own vocabulary through:
// a task's status is the status name of its List's workflow, and priority is ClickUp's 1 (urgent)
// through 4 (low).
//
// The taskEvent Trigger serves one webhook endpoint per connection. It verifies each request's
// X-Signature, the hex HMAC-SHA256 of the body under the webhook's secret, and records the event in
// every accepting binding's durable inbox before answering 200.
package clickup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

// APIBaseURL is the ClickUp API v2 base URL that every request path is appended to.
const APIBaseURL = "https://api.clickup.com/api/v2"

// PersonalAPITokenPrefix starts every ClickUp personal API token.
const PersonalAPITokenPrefix = "pk_"

const (
	providerName = "clickup"

	rateLimitResetHeader = "X-RateLimit-Reset"

	// defaultRequestTimeout keeps a write and its read-back inside the 30-second Execute timeout.
	defaultRequestTimeout = 9 * time.Second
	// operationBudget bounds an operation that sends several requests, below the Execute timeout.
	operationBudget = 25 * time.Second
	// maximumRateLimitDelay bounds a wait for X-RateLimit-Reset; ClickUp limits requests per minute.
	maximumRateLimitDelay = time.Minute
)

var (
	errorCodePattern          = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,15}_[0-9]{1,6}$`)
	errClickUpRequestNotBuilt = errors.New("ClickUp request could not be built")
	// teamNotAuthorizedErrorCodes are ClickUp's codes for a Workspace the token was not authorized for.
	teamNotAuthorizedErrorCodes = teamNotAuthorizedCodes()
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	apiBaseURL string
	now        func() time.Time
	logger     *slog.Logger
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 9 seconds per request. The caller
// retains ownership of the client and its transport. The connector uses a copy that never follows
// redirects, so the token is never replayed to another host.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAPIBaseURL replaces APIBaseURL for a local ClickUp-compatible fake. The URL must use HTTPS unless
// its host is loopback, and it must not carry user information, a query, or a fragment. Production
// connections leave it unset.
func WithAPIBaseURL(baseURL string) Option {
	return func(options *clientOptions) { options.apiBaseURL = baseURL }
}

// WithClock replaces the clock used for receipts, rate-limit waits, write reconciliation, and webhook
// receipt times. Tests use it; production connections leave it unset.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// WithLogger sends the webhook endpoint's delivery records, and those of the durable inboxes that
// NewProjectTaskEventEndpointRunner creates, to logger. Without it, those records go to slog.Default().
// Records carry event IDs, never task content or secrets.
func WithLogger(logger *slog.Logger) Option {
	return func(options *clientOptions) { options.logger = logger }
}

// Client executes authenticated ClickUp API requests for connector operations and serves the taskEvent
// webhook endpoint. A Client is safe for concurrent use by several Steps.
type Client struct {
	apiBaseURL          string
	httpClient          *http.Client
	credentials         sdkgo.CredentialProvider[Credentials]
	maxResponseBytes    int64
	webhookMaxBodyBytes int64
	now                 func() time.Time
	logger              *slog.Logger

	taskEventEndpointsMu sync.Mutex
	taskEventEndpoints   map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, TaskEvent]
}

type clickupRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

type clickupResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

// exchangeOutcome is the provider-neutral meaning of one ClickUp request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeNotFound
	// exchangeNotSent means the connection never opened, so ClickUp cannot have received the request.
	exchangeNotSent
	// exchangeRateLimited is a 429, which ClickUp answers before applying the request.
	exchangeRateLimited
	// exchangeUnconfirmed is a lost connection, unreadable response, 408, or 5xx: ClickUp may have applied it.
	exchangeUnconfirmed
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// clickupExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type clickupExchange struct {
	outcome    exchangeOutcome
	response   clickupResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// isRetryableRead reports an outcome that a read, or a write that is safe to repeat, may simply repeat.
func (result clickupExchange) isRetryableRead() bool {
	switch result.outcome {
	case exchangeNotSent, exchangeRateLimited, exchangeUnconfirmed:
		return true
	default:
		return false
	}
}

// New validates configuration and constructs a ClickUp client. Credentials are resolved before every
// provider request, so a regenerated token takes effect without a restart; the size limits are
// startup configuration.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.MaxResponseBytes < 1 || config.WebhookMaxBodyBytes < 1 {
		return nil, errors.New("ClickUp response and webhook body limits must be positive")
	}
	if credentials == nil {
		return nil, errors.New("ClickUp credential provider is required")
	}
	dependencies := clientOptions{now: time.Now, apiBaseURL: APIBaseURL}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("ClickUp connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("ClickUp connector clock is required")
	}
	apiBaseURL, err := providerhttp.ValidateBaseURL(dependencies.apiBaseURL)
	if err != nil {
		return nil, fmt.Errorf("ClickUp API base URL: %w", err)
	}
	return &Client{
		apiBaseURL: apiBaseURL, credentials: credentials,
		httpClient:       providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		maxResponseBytes: config.MaxResponseBytes, webhookMaxBodyBytes: config.WebhookMaxBodyBytes,
		now: dependencies.now, logger: dependencies.logger,
		taskEventEndpoints: make(map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, TaskEvent]),
	}, nil
}

// SearchTasks returns the searchTasks Query bound to this client.
func (client *Client) SearchTasks() SearchTasksOperation { return SearchTasksOperation{client: client} }

// GetTask returns the getTask Query bound to this client.
func (client *Client) GetTask() GetTaskOperation { return GetTaskOperation{client: client} }

// CreateTask returns the createTask Mutation bound to this client.
func (client *Client) CreateTask() CreateTaskOperation { return CreateTaskOperation{client: client} }

// UpdateTask returns the updateTask Mutation bound to this client.
func (client *Client) UpdateTask() UpdateTaskOperation { return UpdateTaskOperation{client: client} }

// UpdateTaskTags returns the updateTaskTags Mutation bound to this client.
func (client *Client) UpdateTaskTags() UpdateTaskTagsOperation {
	return UpdateTaskTagsOperation{client: client}
}

// AddComment returns the addComment Mutation bound to this client.
func (client *Client) AddComment() AddCommentOperation { return AddCommentOperation{client: client} }

// FindMemberByEmail returns the findMemberByEmail Query bound to this client.
func (client *Client) FindMemberByEmail() FindMemberByEmailOperation {
	return FindMemberByEmailOperation{client: client}
}

// TaskEventWebhookHandler returns the connection's webhook handler for an application to mount at the
// public HTTPS endpoint registered with ClickUp's Create Webhook request. Every taskEvent Trigger built
// from this Connection feeds from it, and it answers 503 while none of them runs, so ClickUp retries.
func (connection Connection) TaskEventWebhookHandler() (http.Handler, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	return connection.client.taskEventWebhookEndpoint(connection.reference)
}

// taskEventTriggerSource is the generated Trigger source hook; an invalid binding is a startup defect.
func (client *Client) taskEventTriggerSource(
	connection sdkgo.ConnectionRef,
	configuration TaskEventTriggerConfiguration,
) sdkgo.TriggerSource[TaskEvent] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	endpoint, err := client.taskEventWebhookEndpoint(connection)
	if err != nil {
		panic(err)
	}
	return endpoint.NewSource(func(event sdkgo.TriggerEvent[TaskEvent]) bool {
		return configuration.acceptsEvent(event.Payload.Event)
	})
}

// taskEventWebhookEndpoint returns the connection's shared endpoint, creating it on first use.
func (client *Client) taskEventWebhookEndpoint(
	connection sdkgo.ConnectionRef,
) (*webhooktrigger.Endpoint[Credentials, TaskEvent], error) {
	client.taskEventEndpointsMu.Lock()
	defer client.taskEventEndpointsMu.Unlock()
	if endpoint, isFound := client.taskEventEndpoints[connection]; isFound {
		return endpoint, nil
	}
	endpoint, err := webhooktrigger.NewEndpoint(webhooktrigger.EndpointConfig[Credentials, TaskEvent]{
		ConnectorID: ConnectorID, TriggerName: TaskEventTriggerDefinition.Trigger.TriggerName,
		Connection: connection, Credentials: client.credentials, MaxBodyBytes: client.webhookMaxBodyBytes,
		VerifyRequest: verifyTaskEventRequest, DecodeEvent: decodeTaskEventRequest,
		Now: client.now, Logger: client.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("ClickUp webhook endpoint: %w", err)
	}
	client.taskEventEndpoints[connection] = endpoint
	return endpoint, nil
}

// resolveCredentials returns a header-safe personal API token, or a safe defect Failure.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return Credentials{}, clickupFailurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable")
	}
	if err := validateResolvedCredentials(credentials); err != nil {
		return Credentials{}, clickupFailurePointer(sdkgo.FailureAuthentication, operation, err.Error())
	}
	return credentials, nil
}

// exchange sends one authenticated request and classifies its response without deciding retry policy.
func (client *Client) exchange(ctx context.Context, credentials Credentials, operation string, request clickupRequest) clickupExchange {
	httpRequest, err := client.buildRequest(ctx, credentials, request)
	if err != nil {
		return clickupExchange{outcome: exchangeDefect, failure: clickupFailure(sdkgo.FailureLocalDefect, operation, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if isConnectionNeverEstablished(err) {
			return clickupExchange{outcome: exchangeNotSent, failure: clickupFailure(sdkgo.FailureTransport, operation, "ClickUp could not be reached; no request was sent")}
		}
		return clickupExchange{outcome: exchangeUnconfirmed, failure: clickupFailure(sdkgo.FailureTransport, operation, "ClickUp request failed before a response arrived")}
	}
	body, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	closeErr := httpResponse.Body.Close()
	response := clickupResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge) && isSuccess:
		return clickupExchange{outcome: exchangeInvalid, response: response, failure: clickupFailure(sdkgo.FailureResponseTooLarge, operation, "ClickUp response exceeds the configured maxResponseBytes limit")}
	case readErr != nil && !errors.Is(readErr, providerhttp.ErrBodyTooLarge), closeErr != nil:
		return clickupExchange{outcome: exchangeUnconfirmed, response: response, failure: clickupFailure(sdkgo.FailureTransport, operation, "ClickUp response could not be read")}
	}
	response.body = body
	if isSuccess {
		if bytes.Contains(body, []byte(credentials.APIToken.Reveal())) {
			response.body = nil
			return clickupExchange{outcome: exchangeInvalid, response: response, failure: clickupFailure(sdkgo.FailureProtocol, operation, "ClickUp response reflected the connection credential")}
		}
		return clickupExchange{outcome: exchangeSucceeded, response: response}
	}
	return client.classifyFailureStatus(response, operation)
}

func (client *Client) buildRequest(ctx context.Context, credentials Credentials, request clickupRequest) (*http.Request, error) {
	var body *bytes.Reader
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return nil, errClickUpRequestNotBuilt
		}
		body = bytes.NewReader(encoded)
	}
	target := client.apiBaseURL + request.path
	if len(request.query) != 0 {
		target += "?" + request.query.Encode()
	}
	var httpRequest *http.Request
	var err error
	if body != nil {
		httpRequest, err = http.NewRequestWithContext(ctx, request.method, target, body)
	} else {
		httpRequest, err = http.NewRequestWithContext(ctx, request.method, target, nil)
	}
	if err != nil {
		return nil, errClickUpRequestNotBuilt
	}
	// ClickUp documents personal tokens as the raw Authorization value, without a Bearer scheme.
	httpRequest.Header.Set("Authorization", credentials.APIToken.Reveal())
	httpRequest.Header.Set("Accept", "application/json")
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	return httpRequest, nil
}

// classifyFailureStatus maps a non-2xx response using only ClickUp's ECODE, never its err text.
func (client *Client) classifyFailureStatus(response clickupResponse, operation string) clickupExchange {
	errorCode := readClickUpErrorCode(response.body)
	response.body = nil
	withCode := func(message string) string {
		if errorCode == "" {
			return message
		}
		return message + " [" + errorCode + "]"
	}
	switch status := response.statusCode; {
	case status == http.StatusTooManyRequests:
		return clickupExchange{outcome: exchangeRateLimited, response: response, retryAfter: client.rateLimitDelay(response.header),
			failure: clickupFailure(sdkgo.FailureRateLimit, operation, withCode("ClickUp rate limited the token (HTTP 429)"))}
	case status == http.StatusRequestTimeout || status >= 500:
		return clickupExchange{outcome: exchangeUnconfirmed, response: response,
			failure: clickupFailure(sdkgo.FailureAvailability, operation, withCode(fmt.Sprintf("ClickUp could not complete the request (HTTP %d)", status)))}
	case status == http.StatusNotFound || status == http.StatusGone:
		return clickupExchange{outcome: exchangeNotFound, response: response,
			failure: clickupFailure(sdkgo.FailureNotFound, operation, withCode(fmt.Sprintf("ClickUp found no such resource (HTTP %d)", status)))}
	case status == http.StatusUnauthorized && teamNotAuthorizedErrorCodes[errorCode]:
		return clickupExchange{outcome: exchangeNotFound, response: response,
			failure: clickupFailure(sdkgo.FailureAuthorization, operation, withCode("ClickUp reports that the resource's Workspace is not authorized for the token (HTTP 401)"))}
	case status >= 300 && status < 400:
		return clickupExchange{outcome: exchangeRejected, response: response,
			failure: clickupFailure(sdkgo.FailureProtocol, operation, fmt.Sprintf("ClickUp redirected the request (HTTP %d)", status))}
	case status == http.StatusUnauthorized:
		return clickupExchange{outcome: exchangeRejected, response: response,
			failure: clickupFailure(sdkgo.FailureAuthentication, operation, withCode("ClickUp rejected the personal API token (HTTP 401)"))}
	default:
		return clickupExchange{outcome: exchangeRejected, response: response,
			failure: clickupFailure(rejectionFailureKind(status), operation, withCode(fmt.Sprintf("ClickUp rejected the request (HTTP %d)", status)))}
	}
}

// rateLimitDelay waits until X-RateLimit-Reset, a Unix time in seconds, or a Retry-After, within a bound.
func (client *Client) rateLimitDelay(header http.Header) time.Duration {
	now := client.now()
	if delay := providerhttp.ParseRetryAfter(header.Get("Retry-After"), now); delay > 0 {
		return min(delay, maximumRateLimitDelay)
	}
	reset, err := strconv.ParseInt(strings.TrimSpace(header.Get(rateLimitResetHeader)), 10, 64)
	if err != nil || reset <= 0 {
		return 0
	}
	delay := time.Unix(reset, 0).Sub(now)
	if delay < time.Second {
		return time.Second
	}
	return min(delay, maximumRateLimitDelay)
}

func (client *Client) receipt(call sdkgo.Call, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
}

func rejectionFailureKind(status int) sdkgo.FailureKind {
	switch status {
	case http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case http.StatusConflict:
		return sdkgo.FailureConflict
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}

// readClickUpErrorCode returns the ECODE of a ClickUp error body; the err text is provider prose and is never read.
func readClickUpErrorCode(body []byte) string {
	var document struct {
		ErrorCode string `json:"ECODE"`
	}
	if json.Unmarshal(body, &document) != nil || !errorCodePattern.MatchString(document.ErrorCode) {
		return ""
	}
	return document.ErrorCode
}

func teamNotAuthorizedCodes() map[string]bool {
	codes := map[string]bool{"OAUTH_023": true, "OAUTH_026": true, "OAUTH_027": true}
	for number := 29; number <= 45; number++ {
		codes[fmt.Sprintf("OAUTH_%03d", number)] = true
	}
	return codes
}

// isConnectionNeverEstablished reports a dial failure, after which ClickUp cannot have received the request.
func isConnectionNeverEstablished(err error) bool {
	var operationError *net.OpError
	return errors.As(err, &operationError) && operationError.Op == "dial"
}

func validateResolvedCredentials(credentials Credentials) error {
	if err := credentials.Validate(); err != nil {
		return errors.New("ClickUp api_token is required")
	}
	token := credentials.APIToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(token) || !strings.HasPrefix(token, PersonalAPITokenPrefix) || len(token) == len(PersonalAPITokenPrefix) {
		return errors.New("ClickUp api_token must be a personal API token that starts with pk_ and has no spaces")
	}
	return nil
}

func clickupFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func clickupFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := clickupFailure(kind, operation, message)
	return &failure
}

// withOperationBudget bounds an operation that sends several requests below the Execute timeout.
func withOperationBudget(call sdkgo.Call) (context.Context, context.CancelFunc) {
	return context.WithTimeout(call.Context, operationBudget)
}

// containsDuplicate reports whether values lists one value twice.
func containsDuplicate[T comparable](values []T) bool {
	seen := make(map[T]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}
