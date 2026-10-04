// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package reamaze implements Re:amaze conversation operations as Dex connector Steps:
// searchConversations and getConversation read conversations, findContactByEmail resolves a
// customer, updateConversation changes status, assignee, and tags so that a repeated attempt
// writes nothing twice, and createConversation and replyToConversation send each request once
// per Step execution and reconcile an unconfirmed attempt by reading the record back, because
// Re:amaze documents no idempotency key. Dex accepts the dispatch checkpoint when the Worker
// writes it to its stream, so a Worker lost before Dex stored it can still send twice.
//
// The connector authenticates as one staff user over HTTP Basic, the user's login email as the
// user name and the user's API token as the password, and sends every request to
// https://{brand}.reamaze.io/api/v1 with Accept: application/json. Statuses, message
// visibilities, origins, and channel types are Re:amaze's own integers, never remapped to
// another vocabulary.
package reamaze

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
	providerName = "reamaze"

	// defaultRequestTimeout keeps an operation's two requests inside the 30-second Execute timeout.
	defaultRequestTimeout = 12 * time.Second

	// requestIDHeader is not in Re:amaze's API documentation, so the Receipt carries it only when present.
	requestIDHeader = "X-Request-Id"

	// rateLimitWindow is Re:amaze's documented per-minute limit; a 429 without Retry-After waits one window.
	rateLimitWindow = time.Minute
)

var (
	brandPattern              = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	requestIDPattern          = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	errReamazeRequestNotBuilt = errors.New("Re:amaze request could not be built")

	// errReamazeCredentialsUnavailable is a credential read that may still succeed, such as a storage outage.
	errReamazeCredentialsUnavailable = errors.New("connection credentials could not be loaded yet; nothing was sent")
	errReamazeCredentialsUnusable    = errors.New("connection credentials are unavailable")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	apiBaseURL string
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 12 seconds per request.
// The caller retains ownership of the client and its transport. The connector uses a copy
// that never follows redirects, so the API token is never replayed to another host.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAPIBaseURL replaces https://{brand}.reamaze.io/api/v1 for a local Re:amaze-compatible
// fake. The URL must use HTTPS unless its host is loopback, and it must not carry user
// information, a query, or a fragment. Production connections leave it unset.
func WithAPIBaseURL(baseURL string) Option {
	return func(options *clientOptions) { options.apiBaseURL = baseURL }
}

// Client executes authenticated Re:amaze API v1 requests for connector operations.
// A Client is safe for concurrent use by several Steps.
type Client struct {
	apiBaseURL       string
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
	now              func() time.Time
}

type reamazeRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

type reamazeResponse struct {
	statusCode int
	header     http.Header
	body       []byte
	requestID  string
}

// exchangeOutcome is the provider-neutral meaning of one Re:amaze request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeNotFound
	// exchangeRateLimited is a 429, which Re:amaze documents as its answer once the per-minute limit is reached.
	exchangeRateLimited
	// exchangeNotSent is a connection that failed before any request byte reached Re:amaze.
	exchangeNotSent
	// exchangeUnavailable is a 5xx, a 408, or a transport failure after connecting; a write may have been applied.
	exchangeUnavailable
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// reamazeExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type reamazeExchange struct {
	outcome    exchangeOutcome
	response   reamazeResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// New validates configuration and constructs a Re:amaze client.
// Credentials are resolved before every provider request, so a replaced token takes effect
// without a restart; the brand and response limit are startup configuration.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !brandPattern.MatchString(config.Brand) {
		return nil, errors.New("Re:amaze brand must be lowercase letters, digits, and hyphens, such as acme for https://acme.reamaze.io")
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Re:amaze response limit must be positive")
	}
	if credentials == nil {
		return nil, errors.New("Re:amaze credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Re:amaze connector option is nil")
		}
		option(&dependencies)
	}
	apiBaseURL := "https://" + config.Brand + ".reamaze.io/api/v1"
	if dependencies.apiBaseURL != "" {
		validated, err := providerhttp.ValidateBaseURL(dependencies.apiBaseURL)
		if err != nil {
			return nil, fmt.Errorf("Re:amaze API base URL: %w", err)
		}
		apiBaseURL = validated
	}
	return &Client{
		apiBaseURL:  apiBaseURL,
		httpClient:  providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials: credentials, maxResponseBytes: config.MaxResponseBytes, now: time.Now,
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

// CreateConversation returns the createConversation Mutation bound to this client.
func (client *Client) CreateConversation() CreateConversationOperation {
	return CreateConversationOperation{client: client}
}

// UpdateConversation returns the updateConversation Mutation bound to this client.
func (client *Client) UpdateConversation() UpdateConversationOperation {
	return UpdateConversationOperation{client: client}
}

// ReplyToConversation returns the replyToConversation Mutation bound to this client.
func (client *Client) ReplyToConversation() ReplyToConversationOperation {
	return ReplyToConversationOperation{client: client}
}

// FindContactByEmail returns the findContactByEmail Query bound to this client.
func (client *Client) FindContactByEmail() FindContactByEmailOperation {
	return FindContactByEmailOperation{client: client}
}

// resolveCredentials returns a header-safe email and token; errReamazeCredentialsUnavailable marks a read that may recover.
func (client *Client) resolveCredentials(call sdkgo.Call) (Credentials, error) {
	credentials, err := client.credentials.Resolve(call)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return Credentials{}, errReamazeCredentialsUnusable
	case err != nil:
		return Credentials{}, errReamazeCredentialsUnavailable
	case validateResolvedCredentials(credentials) != nil:
		return Credentials{}, errReamazeCredentialsUnusable
	}
	return credentials, nil
}

// exchange sends one authenticated request and classifies its response without deciding retry policy.
func (client *Client) exchange(call sdkgo.Call, credentials Credentials, operation string, request reamazeRequest) reamazeExchange {
	httpRequest, err := client.buildRequest(call, credentials, request)
	if err != nil {
		return reamazeExchange{outcome: exchangeDefect, failure: reamazeFailure(sdkgo.FailureLocalDefect, operation, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if isConnectionNeverEstablished(err) {
			return reamazeExchange{outcome: exchangeNotSent, failure: reamazeFailure(sdkgo.FailureTransport, operation, "Re:amaze could not be reached; no request was sent")}
		}
		return reamazeExchange{outcome: exchangeUnavailable, failure: reamazeFailure(sdkgo.FailureTransport, operation, "Re:amaze request failed before a response arrived")}
	}
	body, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	closeErr := httpResponse.Body.Close()
	response := reamazeResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header, requestID: safeRequestID(httpResponse.Header)}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge) && isSuccess:
		return reamazeExchange{outcome: exchangeInvalid, response: response, failure: reamazeFailure(sdkgo.FailureResponseTooLarge, operation, "Re:amaze response exceeds the configured maxResponseBytes limit")}
	case readErr != nil && !errors.Is(readErr, providerhttp.ErrBodyTooLarge), closeErr != nil:
		return reamazeExchange{outcome: exchangeUnavailable, response: response, failure: reamazeFailure(sdkgo.FailureTransport, operation, "Re:amaze response could not be read")}
	}
	if isSuccess {
		if bytes.Contains(body, []byte(credentials.APIToken.Reveal())) {
			return reamazeExchange{outcome: exchangeInvalid, response: response, failure: reamazeFailure(sdkgo.FailureProtocol, operation, "Re:amaze response reflected the connection credential")}
		}
		response.body = body
		return reamazeExchange{outcome: exchangeSucceeded, response: response}
	}
	return client.classifyFailureStatus(response, operation)
}

func (client *Client) buildRequest(call sdkgo.Call, credentials Credentials, request reamazeRequest) (*http.Request, error) {
	target := client.apiBaseURL + request.path
	if len(request.query) != 0 {
		target += "?" + request.query.Encode()
	}
	var httpRequest *http.Request
	var err error
	if request.payload != nil {
		encoded, encodeErr := json.Marshal(request.payload)
		if encodeErr != nil {
			return nil, errReamazeRequestNotBuilt
		}
		httpRequest, err = http.NewRequestWithContext(call.Context, request.method, target, bytes.NewReader(encoded))
	} else {
		httpRequest, err = http.NewRequestWithContext(call.Context, request.method, target, nil)
	}
	if err != nil {
		return nil, errReamazeRequestNotBuilt
	}
	httpRequest.SetBasicAuth(credentials.Email, credentials.APIToken.Reveal())
	httpRequest.Header.Set("Accept", "application/json")
	if request.payload != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	return httpRequest, nil
}

// classifyFailureStatus maps a non-2xx response by status alone; Re:amaze error bodies are never read.
func (client *Client) classifyFailureStatus(response reamazeResponse, operation string) reamazeExchange {
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
	switch status := response.statusCode; {
	case status == http.StatusTooManyRequests:
		if retryAfter <= 0 {
			retryAfter = rateLimitWindow
		}
		return reamazeExchange{outcome: exchangeRateLimited, response: response, retryAfter: retryAfter,
			failure: reamazeFailure(sdkgo.FailureRateLimit, operation, "Re:amaze rate limited the request (HTTP 429)")}
	case status == http.StatusRequestTimeout || status >= 500:
		return reamazeExchange{outcome: exchangeUnavailable, response: response, retryAfter: retryAfter,
			failure: reamazeFailure(sdkgo.FailureAvailability, operation, fmt.Sprintf("Re:amaze could not complete the request (HTTP %d)", status))}
	case status == http.StatusNotFound:
		return reamazeExchange{outcome: exchangeNotFound, response: response,
			failure: reamazeFailure(sdkgo.FailureNotFound, operation, "Re:amaze found no such resource in the brand (HTTP 404)")}
	case status >= 300 && status < 400:
		return reamazeExchange{outcome: exchangeRejected, response: response,
			failure: reamazeFailure(sdkgo.FailureProtocol, operation, fmt.Sprintf("Re:amaze redirected the request (HTTP %d); check that brand is the brand's reamaze.io subdomain", status))}
	default:
		return reamazeExchange{outcome: exchangeRejected, response: response,
			failure: reamazeFailure(rejectionFailureKind(status), operation, fmt.Sprintf("Re:amaze rejected the request (HTTP %d)", status))}
	}
}

func rejectionFailureKind(status int) sdkgo.FailureKind {
	switch status {
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
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

// isRetryableRead reports an outcome after which repeating a read is safe and may succeed.
func (result reamazeExchange) isRetryableRead() bool {
	switch result.outcome {
	case exchangeRateLimited, exchangeNotSent, exchangeUnavailable:
		return true
	default:
		return false
	}
}

func (client *Client) receipt(call sdkgo.Call, response reamazeResponse, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderRequestID: response.requestID, ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
}

// isConnectionNeverEstablished reports a dial failure, after which Re:amaze cannot have received the request.
func isConnectionNeverEstablished(err error) bool {
	var operationError *net.OpError
	return errors.As(err, &operationError) && operationError.Op == "dial"
}

func safeRequestID(header http.Header) string {
	requestID := header.Get(requestIDHeader)
	if requestIDPattern.MatchString(requestID) {
		return requestID
	}
	return ""
}

func conversationPath(conversationID string) string {
	return "/conversations/" + url.PathEscape(conversationID)
}

func validateResolvedCredentials(credentials Credentials) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	if !isBareEmailAddress(credentials.Email) {
		return errors.New("Re:amaze login email must be one bare address")
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.APIToken.Reveal()) {
		return errors.New("Re:amaze API token must be printable ASCII without spaces")
	}
	return nil
}

// isBareEmailAddress accepts one plain address; a colon would split the HTTP Basic user name.
func isBareEmailAddress(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value && !strings.ContainsAny(value, " \"()<>,;:[]\\")
}

// credentialQueryAttempt retries a credential read that may recover; unusable credentials select defect.
func credentialQueryAttempt[OUT any](operation string, defect sdkgo.BranchID, err error) sdkgo.QueryAttempt[OUT] {
	if errors.Is(err, errReamazeCredentialsUnavailable) {
		return sdkgo.NewQueryRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, operation, err.Error()), 0)
	}
	var zero OUT
	return sdkgo.NewQueryBranch(defect, zero, reamazeFailurePointer(sdkgo.FailureAuthentication, operation, err.Error()), sdkgo.Receipt{})
}

// credentialMutationAttempt runs before any request is sent, so retrying is safe for every Mutation.
func credentialMutationAttempt[OUT any](operation string, defect sdkgo.BranchID, value OUT, err error) sdkgo.MutationAttempt[OUT] {
	if errors.Is(err, errReamazeCredentialsUnavailable) {
		return sdkgo.NewMutationRetry[OUT](reamazeFailure(sdkgo.FailureAvailability, operation, err.Error()), 0)
	}
	return sdkgo.NewMutationBranch(defect, value, reamazeFailurePointer(sdkgo.FailureAuthentication, operation, err.Error()), sdkgo.Receipt{})
}

func reamazeFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func reamazeFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := reamazeFailure(kind, operation, message)
	return &failure
}
