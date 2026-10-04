// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package hiver implements Hiver shared-inbox operations as Dex connector Steps:
// listInboxes, listConversations, and getConversation read shared inboxes and their
// conversations; updateConversation sets a conversation's status, assignee, and tags
// with an absolute change that is safe to repeat and reads the conversation back; and
// addNote and createSharedDraft add an internal note or a shared reply draft. Hiver
// documents no idempotency key, so those two run with sync durability and record a Dex
// dispatch checkpoint before sending; a retry that finds it selects uncertain instead of
// sending again. Dex may not yet have stored the checkpoint when the request leaves, so a
// Worker that loses its Dex connection in that instant can still send a second note or draft.
//
// The connector sends an admin-created API key as a bearer token to
// https://api2.hiverhq.com/v1 and spaces its own requests, because Hiver allows one
// request per second per account. After a 429 it holds every later request of the
// client. Statuses keep Hiver's own open, pending, and closed vocabulary.
package hiver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "hiver"

	// productionAPIBaseURL is the only host Hiver documents for its REST API.
	productionAPIBaseURL = "https://api2.hiverhq.com/v1"

	// defaultRequestTimeout bounds one request; Execute timeouts cover several spaced requests.
	defaultRequestTimeout = 12 * time.Second

	// requestIDHeader is not in Hiver's API documentation, so the Receipt carries it only when present.
	requestIDHeader = "X-Request-Id"

	maximumPageTokenBytes = 4096
)

var (
	hiverIDPattern          = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	pageTokenPattern        = regexp.MustCompile(`^[A-Za-z0-9+/=_.-]+$`)
	requestIDPattern        = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	errHiverRequestNotBuilt = errors.New("Hiver request could not be built")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	apiBaseURL string
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 12 seconds per request.
// The caller retains ownership of the client and its transport. The connector uses a copy
// that never follows redirects, so the API key is never replayed to another host.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAPIBaseURL replaces https://api2.hiverhq.com/v1 for a local Hiver-compatible fake. The
// URL must use HTTPS unless its host is loopback, and it must not carry user information, a
// query, or a fragment. Production connections leave it unset.
func WithAPIBaseURL(baseURL string) Option {
	return func(options *clientOptions) { options.apiBaseURL = baseURL }
}

// Client executes authenticated Hiver API v1 requests for connector operations.
// A Client is safe for concurrent use by several Steps; their requests share one spacing
// schedule, so together they stay within the connection's configured request interval.
// A 429 holds that schedule for the longer of Retry-After and a penalty of four intervals
// that doubles with each further 429, at most one minute; any other answer resets the penalty.
type Client struct {
	apiBaseURL       string
	httpClient       *http.Client
	credentials      CredentialSource
	maxResponseBytes int64
	pacer            *requestPacer
	now              func() time.Time
}

type hiverRequest struct {
	method      string
	path        string
	query       url.Values
	jsonPayload any
	// formFields sends a multipart/form-data body, which Hiver documents for notes and shared drafts.
	formFields []hiverFormField
}

type hiverFormField struct {
	name  string
	value string
}

type hiverResponse struct {
	statusCode int
	header     http.Header
	body       []byte
	requestID  string
}

// exchangeOutcome is the provider-neutral meaning of one Hiver request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeNotFound
	// exchangeRateLimited is a 429, which Hiver returns instead of processing the request.
	exchangeRateLimited
	// exchangeNotSent means no request byte reached Hiver: a refused connection or a cancelled wait for a request slot.
	exchangeNotSent
	// exchangeUnavailable is a 5xx, a 408, or a transport failure after connecting; a write may have been applied.
	exchangeUnavailable
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// hiverExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type hiverExchange struct {
	outcome    exchangeOutcome
	response   hiverResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// New validates configuration and constructs a Hiver client.
// Credentials are resolved before every provider request, so a replaced API key takes
// effect without a restart; the response limit and request interval are startup configuration.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Hiver response limit must be positive")
	}
	if config.RequestIntervalMilliseconds < 1 || config.RequestIntervalMilliseconds > 60000 {
		return nil, errors.New("Hiver request interval must be between 1 and 60000 milliseconds")
	}
	if credentials == nil {
		return nil, errors.New("Hiver credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Hiver connector option is nil")
		}
		option(&dependencies)
	}
	apiBaseURL := productionAPIBaseURL
	if dependencies.apiBaseURL != "" {
		validated, err := providerhttp.ValidateBaseURL(dependencies.apiBaseURL)
		if err != nil {
			return nil, fmt.Errorf("Hiver API base URL: %w", err)
		}
		apiBaseURL = validated
	}
	return &Client{
		apiBaseURL: apiBaseURL, httpClient: providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials: credentials, maxResponseBytes: config.MaxResponseBytes,
		pacer: newRequestPacer(time.Duration(config.RequestIntervalMilliseconds)*time.Millisecond, time.Now), now: time.Now,
	}, nil
}

// ListInboxes returns the listInboxes Query bound to this client.
func (client *Client) ListInboxes() ListInboxesOperation { return ListInboxesOperation{client: client} }

// ListConversations returns the listConversations Query bound to this client.
func (client *Client) ListConversations() ListConversationsOperation {
	return ListConversationsOperation{client: client}
}

// GetConversation returns the getConversation Query bound to this client.
func (client *Client) GetConversation() GetConversationOperation {
	return GetConversationOperation{client: client}
}

// UpdateConversation returns the updateConversation Mutation bound to this client.
func (client *Client) UpdateConversation() UpdateConversationOperation {
	return UpdateConversationOperation{client: client}
}

// AddNote returns the addNote Mutation bound to this client.
func (client *Client) AddNote() AddNoteOperation { return AddNoteOperation{client: client} }

// CreateSharedDraft returns the createSharedDraft Mutation bound to this client.
func (client *Client) CreateSharedDraft() CreateSharedDraftOperation {
	return CreateSharedDraftOperation{client: client}
}

// resolveCredentials returns a header-safe API key, or a safe defect Failure.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return Credentials{}, hiverFailurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable")
	}
	return credentials, nil
}

// exchange waits for the connection's next request slot, then sends one request.
func (client *Client) exchange(call sdkgo.Call, credentials Credentials, operation string, request hiverRequest) hiverExchange {
	if result, isAvailable := client.awaitRequestSlot(call, operation); !isAvailable {
		return result
	}
	return client.send(call, credentials, operation, request)
}

// awaitRequestSlot keeps requests one interval apart; a cancelled wait sends nothing.
func (client *Client) awaitRequestSlot(call sdkgo.Call, operation string) (hiverExchange, bool) {
	if err := client.pacer.wait(call.Context); err != nil {
		return hiverExchange{outcome: exchangeNotSent, failure: hiverFailure(sdkgo.FailureAvailability, operation,
			"the Step ended while waiting for the connection's next Hiver request slot; no request was sent")}, false
	}
	return hiverExchange{}, true
}

// send dispatches one authenticated request and classifies its response without deciding retry policy.
func (client *Client) send(call sdkgo.Call, credentials Credentials, operation string, request hiverRequest) hiverExchange {
	httpRequest, err := client.buildRequest(call, credentials, request)
	if err != nil {
		return hiverExchange{outcome: exchangeDefect, failure: hiverFailure(sdkgo.FailureLocalDefect, operation, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if isConnectionNeverEstablished(err) {
			return hiverExchange{outcome: exchangeNotSent, failure: hiverFailure(sdkgo.FailureTransport, operation, "Hiver could not be reached; no request was sent")}
		}
		return hiverExchange{outcome: exchangeUnavailable, failure: hiverFailure(sdkgo.FailureTransport, operation, "Hiver request failed before a response arrived")}
	}
	if httpResponse.StatusCode == http.StatusTooManyRequests {
		client.pacer.holdAfterRateLimit(providerhttp.ParseRetryAfter(httpResponse.Header.Get("Retry-After"), client.now()))
	} else {
		client.pacer.resetRateLimitPenalty()
	}
	body, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	closeErr := httpResponse.Body.Close()
	response := hiverResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header, requestID: safeRequestID(httpResponse.Header)}
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		// The status line decides a failure; its body is never used, so a read error cannot change it.
		return client.classifyFailureStatus(response, operation)
	}
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge):
		return hiverExchange{outcome: exchangeInvalid, response: response, failure: hiverFailure(sdkgo.FailureResponseTooLarge, operation, "Hiver response exceeds the configured maxResponseBytes limit")}
	case readErr != nil, closeErr != nil:
		return hiverExchange{outcome: exchangeUnavailable, response: response, failure: hiverFailure(sdkgo.FailureTransport, operation, "Hiver response could not be read")}
	}
	if bytes.Contains(body, []byte(credentials.APIKey.Reveal())) {
		return hiverExchange{outcome: exchangeInvalid, response: response, failure: hiverFailure(sdkgo.FailureProtocol, operation, "Hiver response reflected the connection credential")}
	}
	response.body = body
	return hiverExchange{outcome: exchangeSucceeded, response: response}
}

func (client *Client) buildRequest(call sdkgo.Call, credentials Credentials, request hiverRequest) (*http.Request, error) {
	target := client.apiBaseURL + request.path
	if len(request.query) != 0 {
		target += "?" + request.query.Encode()
	}
	body, contentType, err := encodeRequestBody(request)
	if err != nil {
		return nil, errHiverRequestNotBuilt
	}
	httpRequest, err := http.NewRequestWithContext(call.Context, request.method, target, body)
	if err != nil {
		return nil, errHiverRequestNotBuilt
	}
	httpRequest.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	httpRequest.Header.Set("Accept", "application/json")
	if contentType != "" {
		httpRequest.Header.Set("Content-Type", contentType)
	}
	return httpRequest, nil
}

// encodeRequestBody returns a JSON or multipart body, or no body for a request without a payload.
func encodeRequestBody(request hiverRequest) (*bytes.Reader, string, error) {
	switch {
	case request.jsonPayload != nil:
		encoded, err := json.Marshal(request.jsonPayload)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(encoded), "application/json", nil
	case len(request.formFields) != 0:
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		for _, field := range request.formFields {
			if err := writer.WriteField(field.name, field.value); err != nil {
				return nil, "", err
			}
		}
		if err := writer.Close(); err != nil {
			return nil, "", err
		}
		return bytes.NewReader(buffer.Bytes()), writer.FormDataContentType(), nil
	default:
		return bytes.NewReader(nil), "", nil
	}
}

// classifyFailureStatus maps a non-2xx response by status alone: Hiver's error bodies hold only message text.
func (client *Client) classifyFailureStatus(response hiverResponse, operation string) hiverExchange {
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
	switch status := response.statusCode; {
	case status == http.StatusTooManyRequests:
		return hiverExchange{outcome: exchangeRateLimited, response: response, retryAfter: retryAfter,
			failure: hiverFailure(sdkgo.FailureRateLimit, operation, "Hiver rate limited the request (HTTP 429): one request per second or the daily request limit was exceeded")}
	case status == http.StatusRequestTimeout || status >= 500:
		return hiverExchange{outcome: exchangeUnavailable, response: response, retryAfter: retryAfter,
			failure: hiverFailure(sdkgo.FailureAvailability, operation, fmt.Sprintf("Hiver could not complete the request (HTTP %d)", status))}
	case status == http.StatusNotFound:
		return hiverExchange{outcome: exchangeNotFound, response: response,
			failure: hiverFailure(sdkgo.FailureNotFound, operation, "Hiver found no such resource (HTTP 404)")}
	case status >= 300 && status < 400:
		return hiverExchange{outcome: exchangeRejected, response: response,
			failure: hiverFailure(sdkgo.FailureProtocol, operation, fmt.Sprintf("Hiver redirected the request (HTTP %d), which the connector never follows", status))}
	default:
		return hiverExchange{outcome: exchangeRejected, response: response,
			failure: hiverFailure(rejectionFailureKind(status), operation, fmt.Sprintf("Hiver rejected the request (HTTP %d)", status))}
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

func (client *Client) receipt(call sdkgo.Call, response hiverResponse, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderRequestID: response.requestID, ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
}

// isConnectionNeverEstablished reports a dial failure, after which Hiver cannot have received the request.
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

func inboxPath(inboxID string) string {
	return "/inboxes/" + url.PathEscape(inboxID)
}

func conversationPath(inboxID string, conversationID string) string {
	return inboxPath(inboxID) + "/conversations/" + url.PathEscape(conversationID)
}

func validateResolvedCredentials(credentials Credentials) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return errors.New("Hiver API key must be printable ASCII without spaces")
	}
	return nil
}

func validateHiverID(field string, value string) error {
	if !hiverIDPattern.MatchString(value) {
		return fmt.Errorf("%s must be a Hiver ID of 1 to 64 letters, digits, hyphens, or underscores", field)
	}
	return nil
}

func validatePageToken(field string, value string) error {
	if value != "" && !isPageToken(value) {
		return fmt.Errorf("%s must be a next-page token exactly as Hiver returned it", field)
	}
	return nil
}

// isPageToken accepts Hiver's base64 next_page tokens of at most maximumPageTokenBytes.
func isPageToken(value string) bool {
	return len(value) <= maximumPageTokenBytes && pageTokenPattern.MatchString(value)
}

// validateText requires a non-blank value of at most maxBytes without NUL characters.
func validateText(field string, value string, maxBytes int) error {
	switch {
	case strings.TrimSpace(value) == "":
		return fmt.Errorf("%s is required", field)
	case len(value) > maxBytes:
		return fmt.Errorf("%s exceeds %d bytes", field, maxBytes)
	case strings.ContainsRune(value, 0):
		return fmt.Errorf("%s must not contain NUL characters", field)
	}
	return nil
}

func hiverFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func hiverFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := hiverFailure(kind, operation, message)
	return &failure
}
