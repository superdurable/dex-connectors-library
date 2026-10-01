// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package intercom implements Intercom conversation operations and a signed conversation webhook
// Trigger as Dex connector Steps. searchConversations, getConversation, and findContactByEmail read;
// replyToConversation adds an admin reply or note at most once per Step execution; and
// updateConversationState closes, reopens, or snoozes a conversation and is safe to repeat.
//
// The connector authenticates with an Intercom app access token as a bearer token, sends every request
// to the workspace's regional API host with the Intercom-Version header set to APIVersion, and takes
// and returns Intercom's own conversation states: open, closed, and snoozed.
//
// The conversationEvent Trigger serves one webhook endpoint per connection. It answers Intercom's HEAD
// validation request, verifies each notification's X-Hub-Signature with the app's client secret, and
// records it in every accepting binding's durable inbox before answering 200.
package intercom

import (
	"bytes"
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

// APIVersion is the Intercom REST API version every request sends in the Intercom-Version header.
// The connector decodes responses and webhook notifications in this version's shapes.
const APIVersion = "2.16"

const (
	providerName = "intercom"

	intercomVersionHeader = "Intercom-Version"
	rateLimitResetHeader  = "X-RateLimit-Reset"

	// defaultRequestTimeout keeps an updateConversationState read, write, and reread inside the 30-second Execute timeout.
	defaultRequestTimeout = 9 * time.Second
	// maximumRateLimitDelay bounds a wait for X-RateLimit-Reset; Intercom resets its limits every 10 seconds.
	maximumRateLimitDelay = time.Minute

	maximumReportedErrorCodes = 3
)

var (
	regionAPIBaseURLs = map[Region]string{
		RegionUs: "https://api.intercom.io",
		RegionEu: "https://api.eu.intercom.io",
		RegionAu: "https://api.au.intercom.io",
	}
	errorTokenPattern          = regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)
	requestIDPattern           = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	errIntercomRequestNotBuilt = errors.New("Intercom request could not be built")
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
// redirects, so the access token is never replayed to another host.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAPIBaseURL replaces the regional Intercom API host for a local Intercom-compatible fake. The URL
// must use HTTPS unless its host is loopback, and it must not carry user information, a query, or a
// fragment. Production connections leave it unset and choose the host with the region configuration.
func WithAPIBaseURL(baseURL string) Option {
	return func(options *clientOptions) { options.apiBaseURL = baseURL }
}

// WithClock replaces the clock used for receipts, rate-limit waits, reply reconciliation, and webhook
// receipt times. Tests use it; production connections leave it unset.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// WithLogger sends the webhook endpoint's delivery records, and those of the durable inboxes that
// NewLocalConversationEventEndpointRunner creates, to logger. Without it, those records go to
// slog.Default(). Records carry notification IDs, never message text or secrets.
func WithLogger(logger *slog.Logger) Option {
	return func(options *clientOptions) { options.logger = logger }
}

// Client executes authenticated Intercom API requests for connector operations and serves the
// conversationEvent webhook endpoint. A Client is safe for concurrent use by several Steps.
type Client struct {
	apiBaseURL          string
	httpClient          *http.Client
	credentials         sdkgo.CredentialProvider[Credentials]
	maxResponseBytes    int64
	webhookMaxBodyBytes int64
	now                 func() time.Time
	logger              *slog.Logger

	conversationEventEndpointsMu sync.Mutex
	conversationEventEndpoints   map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, ConversationEvent]
}

type intercomRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

type intercomResponse struct {
	statusCode int
	header     http.Header
	body       []byte
	requestID  string
}

// exchangeOutcome is the provider-neutral meaning of one Intercom request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeNotFound
	// exchangeNotSent means the connection never opened, so Intercom cannot have received the request.
	exchangeNotSent
	// exchangeRateLimited is a 429, which Intercom answers before applying the request.
	exchangeRateLimited
	// exchangeUnconfirmed is a lost connection, unreadable response, 408, or 5xx: Intercom may have applied it.
	exchangeUnconfirmed
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// intercomExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type intercomExchange struct {
	outcome    exchangeOutcome
	response   intercomResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// isRetryableRead reports an outcome that a read, which changes nothing, may simply repeat.
func (result intercomExchange) isRetryableRead() bool {
	switch result.outcome {
	case exchangeNotSent, exchangeRateLimited, exchangeUnconfirmed:
		return true
	default:
		return false
	}
}

// New validates configuration and constructs an Intercom client. Credentials are resolved before every
// provider request, so a regenerated access token takes effect without a restart; the region and size
// limits are startup configuration.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	apiBaseURL, isKnownRegion := regionAPIBaseURLs[config.Region]
	if !isKnownRegion {
		return nil, errors.New("Intercom region must be us, eu, or au")
	}
	if config.MaxResponseBytes < 1 || config.WebhookMaxBodyBytes < 1 {
		return nil, errors.New("Intercom response and webhook body limits must be positive")
	}
	if credentials == nil {
		return nil, errors.New("Intercom credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Intercom connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("Intercom connector clock is required")
	}
	if dependencies.apiBaseURL != "" {
		validated, err := providerhttp.ValidateBaseURL(dependencies.apiBaseURL)
		if err != nil {
			return nil, fmt.Errorf("Intercom API base URL: %w", err)
		}
		apiBaseURL = validated
	}
	return &Client{
		apiBaseURL: apiBaseURL, credentials: credentials,
		httpClient:       providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		maxResponseBytes: config.MaxResponseBytes, webhookMaxBodyBytes: config.WebhookMaxBodyBytes,
		now: dependencies.now, logger: dependencies.logger,
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

// UpdateConversationState returns the updateConversationState Mutation bound to this client.
func (client *Client) UpdateConversationState() UpdateConversationStateOperation {
	return UpdateConversationStateOperation{client: client}
}

// FindContactByEmail returns the findContactByEmail Query bound to this client.
func (client *Client) FindContactByEmail() FindContactByEmailOperation {
	return FindContactByEmailOperation{client: client}
}

// ConversationEventWebhookHandler returns the connection's webhook handler for an application to mount
// at the public HTTPS URL entered in the Intercom app's Configure > Webhooks page. It answers Intercom's
// HEAD validation request with 200. Every conversationEvent Trigger built from this Connection feeds
// from it, and it answers 503 to a notification while none of them runs, so Intercom retries.
func (connection Connection) ConversationEventWebhookHandler() (http.Handler, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	endpoint, err := connection.client.conversationEventWebhookEndpoint(connection.reference)
	if err != nil {
		return nil, err
	}
	return headValidatingHandler{next: endpoint}, nil
}

// conversationEventTriggerSource is the generated Trigger source hook; an invalid binding is a startup defect.
func (client *Client) conversationEventTriggerSource(
	connection sdkgo.ConnectionRef,
	configuration ConversationEventTriggerConfiguration,
) sdkgo.TriggerSource[ConversationEvent] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	endpoint, err := client.conversationEventWebhookEndpoint(connection)
	if err != nil {
		panic(err)
	}
	return endpoint.NewSource(func(event sdkgo.TriggerEvent[ConversationEvent]) bool {
		return configuration.acceptsTopic(event.Payload.Topic)
	})
}

// conversationEventWebhookEndpoint returns the connection's shared endpoint, creating it on first use.
func (client *Client) conversationEventWebhookEndpoint(
	connection sdkgo.ConnectionRef,
) (*webhooktrigger.Endpoint[Credentials, ConversationEvent], error) {
	client.conversationEventEndpointsMu.Lock()
	defer client.conversationEventEndpointsMu.Unlock()
	if endpoint, found := client.conversationEventEndpoints[connection]; found {
		return endpoint, nil
	}
	endpoint, err := webhooktrigger.NewEndpoint(webhooktrigger.EndpointConfig[Credentials, ConversationEvent]{
		ConnectorID: ConnectorID, TriggerName: ConversationEventTriggerDefinition.Trigger.TriggerName,
		Connection: connection, Credentials: client.credentials, MaxBodyBytes: client.webhookMaxBodyBytes,
		VerifyRequest: verifyConversationEventRequest, DecodeEvent: decodeConversationEventRequest,
		Now: client.now, Logger: client.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("Intercom webhook endpoint: %w", err)
	}
	client.conversationEventEndpoints[connection] = endpoint
	return endpoint, nil
}

// resolveCredentials returns a header-safe access token, or a safe defect Failure.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return Credentials{}, intercomFailurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable")
	}
	return credentials, nil
}

// exchange sends one authenticated request and classifies its response without deciding retry policy.
func (client *Client) exchange(call sdkgo.Call, credentials Credentials, operation string, request intercomRequest) intercomExchange {
	httpRequest, err := client.buildRequest(call, credentials, request)
	if err != nil {
		return intercomExchange{outcome: exchangeDefect, failure: intercomFailure(sdkgo.FailureLocalDefect, operation, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if isConnectionNeverEstablished(err) {
			return intercomExchange{outcome: exchangeNotSent, failure: intercomFailure(sdkgo.FailureTransport, operation, "Intercom could not be reached; no request was sent")}
		}
		return intercomExchange{outcome: exchangeUnconfirmed, failure: intercomFailure(sdkgo.FailureTransport, operation, "Intercom request failed before a response arrived")}
	}
	body, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	closeErr := httpResponse.Body.Close()
	response := intercomResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge) && isSuccess:
		return intercomExchange{outcome: exchangeInvalid, response: response, failure: intercomFailure(sdkgo.FailureResponseTooLarge, operation, "Intercom response exceeds the configured maxResponseBytes limit")}
	case readErr != nil && !errors.Is(readErr, providerhttp.ErrBodyTooLarge), closeErr != nil:
		return intercomExchange{outcome: exchangeUnconfirmed, response: response, failure: intercomFailure(sdkgo.FailureTransport, operation, "Intercom response could not be read")}
	}
	response.body = body
	if isSuccess {
		if bytes.Contains(body, []byte(credentials.AccessToken.Reveal())) {
			response.body = nil
			return intercomExchange{outcome: exchangeInvalid, response: response, failure: intercomFailure(sdkgo.FailureProtocol, operation, "Intercom response reflected the connection credential")}
		}
		return intercomExchange{outcome: exchangeSucceeded, response: response}
	}
	return client.classifyFailureStatus(response, operation)
}

// readConversation reads one conversation with its parts as plain text.
func (client *Client) readConversation(call sdkgo.Call, credentials Credentials, operation string, conversationID string) intercomExchange {
	return client.exchange(call, credentials, operation, intercomRequest{
		method: http.MethodGet, path: conversationPath(conversationID), query: url.Values{"display_as": {"plaintext"}},
	})
}

func (client *Client) buildRequest(call sdkgo.Call, credentials Credentials, request intercomRequest) (*http.Request, error) {
	var body *bytes.Reader
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return nil, errIntercomRequestNotBuilt
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
		httpRequest, err = http.NewRequestWithContext(call.Context, request.method, target, body)
	} else {
		httpRequest, err = http.NewRequestWithContext(call.Context, request.method, target, nil)
	}
	if err != nil {
		return nil, errIntercomRequestNotBuilt
	}
	httpRequest.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set(intercomVersionHeader, APIVersion)
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	return httpRequest, nil
}

// classifyFailureStatus maps a non-2xx response using only Intercom's error codes and field names.
func (client *Client) classifyFailureStatus(response intercomResponse, operation string) intercomExchange {
	summary := describeIntercomError(response.body)
	response.body = nil
	response.requestID = summary.requestID
	switch status := response.statusCode; {
	case status == http.StatusTooManyRequests:
		return intercomExchange{outcome: exchangeRateLimited, response: response, retryAfter: client.rateLimitDelay(response.header),
			failure: intercomFailure(sdkgo.FailureRateLimit, operation, withErrorSummary("Intercom rate limited the request (HTTP 429)", summary))}
	case status == http.StatusRequestTimeout || status >= 500:
		return intercomExchange{outcome: exchangeUnconfirmed, response: response,
			failure: intercomFailure(sdkgo.FailureAvailability, operation, withErrorSummary(fmt.Sprintf("Intercom could not complete the request (HTTP %d)", status), summary))}
	case status == http.StatusNotFound || status == http.StatusGone:
		return intercomExchange{outcome: exchangeNotFound, response: response,
			failure: intercomFailure(sdkgo.FailureNotFound, operation, withErrorSummary(fmt.Sprintf("Intercom found no such resource (HTTP %d)", status), summary))}
	case status >= 300 && status < 400:
		return intercomExchange{outcome: exchangeRejected, response: response,
			failure: intercomFailure(sdkgo.FailureProtocol, operation, fmt.Sprintf("Intercom redirected the request (HTTP %d); check the region configuration", status))}
	case status == http.StatusUnauthorized:
		return intercomExchange{outcome: exchangeRejected, response: response,
			failure: intercomFailure(sdkgo.FailureAuthentication, operation, withErrorSummary("Intercom rejected the access token (HTTP 401); check the token and that the region matches the workspace", summary))}
	default:
		return intercomExchange{outcome: exchangeRejected, response: response,
			failure: intercomFailure(rejectionFailureKind(status), operation, withErrorSummary(fmt.Sprintf("Intercom rejected the request (HTTP %d)", status), summary))}
	}
}

// rateLimitDelay waits until X-RateLimit-Reset, or a Retry-After if Intercom sends one, within a bound.
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

func (client *Client) receipt(call sdkgo.Call, response intercomResponse, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ProviderRequestID: response.requestID, ObservedAt: client.now().UTC(),
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

// intercomErrorSummary holds only machine-readable error codes, field names, and the request ID.
type intercomErrorSummary struct {
	codes     []string
	fields    []string
	requestID string
}

// describeIntercomError reads an error.list body; error messages are provider text and are never read.
func describeIntercomError(body []byte) intercomErrorSummary {
	var document struct {
		RequestID string `json:"request_id"`
		Errors    []struct {
			Code  string `json:"code"`
			Field string `json:"field"`
		} `json:"errors"`
	}
	summary := intercomErrorSummary{}
	if json.Unmarshal(body, &document) != nil {
		return summary
	}
	if requestIDPattern.MatchString(document.RequestID) {
		summary.requestID = document.RequestID
	}
	for _, entry := range document.Errors {
		if len(summary.codes) == maximumReportedErrorCodes {
			break
		}
		if errorTokenPattern.MatchString(entry.Code) {
			summary.codes = append(summary.codes, entry.Code)
		}
		if errorTokenPattern.MatchString(entry.Field) {
			summary.fields = append(summary.fields, entry.Field)
		}
	}
	return summary
}

func withErrorSummary(message string, summary intercomErrorSummary) string {
	var parts []string
	if len(summary.codes) != 0 {
		parts = append(parts, strings.Join(summary.codes, ", "))
	}
	if len(summary.fields) != 0 {
		parts = append(parts, "fields: "+strings.Join(summary.fields, ", "))
	}
	if len(parts) == 0 {
		return message
	}
	return message + " [" + strings.Join(parts, "; ") + "]"
}

// isConnectionNeverEstablished reports a dial failure, after which Intercom cannot have received the request.
func isConnectionNeverEstablished(err error) bool {
	var operationError *net.OpError
	return errors.As(err, &operationError) && operationError.Op == "dial"
}

func conversationPath(conversationID string) string {
	return "/conversations/" + conversationID
}

func validateResolvedCredentials(credentials Credentials) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Intercom access token must be printable ASCII without spaces")
	}
	return nil
}

func intercomFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func intercomFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := intercomFailure(kind, operation, message)
	return &failure
}
