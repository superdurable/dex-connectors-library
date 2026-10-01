// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package freshdesk implements Freshdesk ticket operations as Dex connector Steps:
// searchTickets and getTicket read tickets, updateTicket changes ticket fields and
// tags so that a repeated attempt writes nothing twice, and createTicket and addNote
// create a ticket or add one note or reply, sending each request at most once per
// Step execution because Freshdesk documents no idempotency key.
//
// The connector authenticates with an agent's API key over HTTP Basic, the key as
// the user name and X as the password, and sends every request to
// https://{domain}.freshdesk.com/api/v2. Statuses and priorities are Freshdesk's own
// integers, never remapped to another vocabulary.
package freshdesk

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "freshdesk"

	// defaultRequestTimeout keeps an operation's two requests inside the 30-second Execute timeout.
	defaultRequestTimeout = 12 * time.Second

	// requestIDHeader is not in Freshdesk's API documentation, so the Receipt carries it only when present.
	requestIDHeader = "X-Request-Id"

	// basicAuthenticationPassword is the dummy password Freshdesk documents for API-key requests.
	basicAuthenticationPassword = "X"

	maximumReportedErrorDetails = 5
)

var (
	domainPattern               = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	errorFieldPattern           = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	requestIDPattern            = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	errFreshdeskRequestNotBuilt = errors.New("Freshdesk request could not be built")
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

// WithAPIBaseURL replaces https://{domain}.freshdesk.com/api/v2 for a local Freshdesk-compatible
// fake. The URL must use HTTPS unless its host is loopback, and it must not carry user
// information, a query, or a fragment. Production connections leave it unset.
func WithAPIBaseURL(baseURL string) Option {
	return func(options *clientOptions) { options.apiBaseURL = baseURL }
}

// Client executes authenticated Freshdesk API v2 requests for connector operations.
// A Client is safe for concurrent use by several Steps.
type Client struct {
	apiBaseURL       string
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
	now              func() time.Time
}

type freshdeskRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

type freshdeskResponse struct {
	statusCode int
	header     http.Header
	body       []byte
	requestID  string
}

// exchangeOutcome is the provider-neutral meaning of one Freshdesk request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeNotFound
	// exchangeRateLimited is a 429, which Freshdesk returns instead of processing the request.
	exchangeRateLimited
	// exchangeNotSent is a connection that failed before any request byte reached Freshdesk.
	exchangeNotSent
	// exchangeUnavailable is a 5xx, a 408, or a transport failure after connecting; a write may have been applied.
	exchangeUnavailable
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// freshdeskExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type freshdeskExchange struct {
	outcome    exchangeOutcome
	response   freshdeskResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// freshdeskErrorSummary holds only Freshdesk's machine-readable error code and field=code pairs.
type freshdeskErrorSummary struct {
	errorCode   string
	fieldErrors []string
}

// New validates configuration and constructs a Freshdesk client.
// Credentials are resolved before every provider request, so a replaced API key takes
// effect without a restart; the domain and response limit are startup configuration.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !domainPattern.MatchString(config.Domain) {
		return nil, errors.New("Freshdesk domain must be lowercase letters, digits, and hyphens, such as acme for https://acme.freshdesk.com")
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Freshdesk response limit must be positive")
	}
	if credentials == nil {
		return nil, errors.New("Freshdesk credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Freshdesk connector option is nil")
		}
		option(&dependencies)
	}
	apiBaseURL := "https://" + config.Domain + ".freshdesk.com/api/v2"
	if dependencies.apiBaseURL != "" {
		validated, err := providerhttp.ValidateBaseURL(dependencies.apiBaseURL)
		if err != nil {
			return nil, fmt.Errorf("Freshdesk API base URL: %w", err)
		}
		apiBaseURL = validated
	}
	return &Client{
		apiBaseURL:  apiBaseURL,
		httpClient:  providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials: credentials, maxResponseBytes: config.MaxResponseBytes, now: time.Now,
	}, nil
}

// SearchTickets returns the searchTickets Query bound to this client.
func (client *Client) SearchTickets() SearchTicketsOperation {
	return SearchTicketsOperation{client: client}
}

// GetTicket returns the getTicket Query bound to this client.
func (client *Client) GetTicket() GetTicketOperation { return GetTicketOperation{client: client} }

// CreateTicket returns the createTicket Mutation bound to this client.
func (client *Client) CreateTicket() CreateTicketOperation {
	return CreateTicketOperation{client: client}
}

// UpdateTicket returns the updateTicket Mutation bound to this client.
func (client *Client) UpdateTicket() UpdateTicketOperation {
	return UpdateTicketOperation{client: client}
}

// AddNote returns the addNote Mutation bound to this client.
func (client *Client) AddNote() AddNoteOperation { return AddNoteOperation{client: client} }

// resolveCredentials returns a header-safe API key, or a safe defect Failure.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return Credentials{}, freshdeskFailurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable")
	}
	return credentials, nil
}

// exchange sends one authenticated request and classifies its response without deciding retry policy.
func (client *Client) exchange(call sdkgo.Call, credentials Credentials, operation string, request freshdeskRequest) freshdeskExchange {
	httpRequest, err := client.buildRequest(call, credentials, request)
	if err != nil {
		return freshdeskExchange{outcome: exchangeDefect, failure: freshdeskFailure(sdkgo.FailureLocalDefect, operation, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if isConnectionNeverEstablished(err) {
			return freshdeskExchange{outcome: exchangeNotSent, failure: freshdeskFailure(sdkgo.FailureTransport, operation, "Freshdesk could not be reached; no request was sent")}
		}
		return freshdeskExchange{outcome: exchangeUnavailable, failure: freshdeskFailure(sdkgo.FailureTransport, operation, "Freshdesk request failed before a response arrived")}
	}
	body, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	closeErr := httpResponse.Body.Close()
	response := freshdeskResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header, requestID: safeRequestID(httpResponse.Header)}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge) && isSuccess:
		return freshdeskExchange{outcome: exchangeInvalid, response: response, failure: freshdeskFailure(sdkgo.FailureResponseTooLarge, operation, "Freshdesk response exceeds the configured maxResponseBytes limit")}
	case readErr != nil && !errors.Is(readErr, providerhttp.ErrBodyTooLarge), closeErr != nil:
		return freshdeskExchange{outcome: exchangeUnavailable, response: response, failure: freshdeskFailure(sdkgo.FailureTransport, operation, "Freshdesk response could not be read")}
	}
	response.body = body
	if isSuccess {
		if bytes.Contains(body, []byte(credentials.APIKey.Reveal())) {
			response.body = nil
			return freshdeskExchange{outcome: exchangeInvalid, response: response, failure: freshdeskFailure(sdkgo.FailureProtocol, operation, "Freshdesk response reflected the connection credential")}
		}
		return freshdeskExchange{outcome: exchangeSucceeded, response: response}
	}
	return client.classifyFailureStatus(response, credentials, operation)
}

func (client *Client) buildRequest(call sdkgo.Call, credentials Credentials, request freshdeskRequest) (*http.Request, error) {
	target := client.apiBaseURL + request.path
	if len(request.query) != 0 {
		target += "?" + request.query.Encode()
	}
	var httpRequest *http.Request
	var err error
	if request.payload != nil {
		encoded, encodeErr := json.Marshal(request.payload)
		if encodeErr != nil {
			return nil, errFreshdeskRequestNotBuilt
		}
		httpRequest, err = http.NewRequestWithContext(call.Context, request.method, target, bytes.NewReader(encoded))
	} else {
		httpRequest, err = http.NewRequestWithContext(call.Context, request.method, target, nil)
	}
	if err != nil {
		return nil, errFreshdeskRequestNotBuilt
	}
	httpRequest.SetBasicAuth(credentials.APIKey.Reveal(), basicAuthenticationPassword)
	httpRequest.Header.Set("Accept", "application/json")
	if request.payload != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	return httpRequest, nil
}

// classifyFailureStatus maps a non-2xx response; Freshdesk documents 429 as a rejection sent instead of processing.
func (client *Client) classifyFailureStatus(response freshdeskResponse, credentials Credentials, operation string) freshdeskExchange {
	summary := describeFreshdeskError(response.body, credentials)
	response.body = nil
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
	switch status := response.statusCode; {
	case status == http.StatusTooManyRequests:
		return freshdeskExchange{outcome: exchangeRateLimited, response: response, retryAfter: retryAfter,
			failure: freshdeskFailure(sdkgo.FailureRateLimit, operation, withErrorSummary("Freshdesk rate limited the request (HTTP 429)", summary))}
	case status == http.StatusRequestTimeout || status >= 500:
		return freshdeskExchange{outcome: exchangeUnavailable, response: response, retryAfter: retryAfter,
			failure: freshdeskFailure(sdkgo.FailureAvailability, operation, withErrorSummary(fmt.Sprintf("Freshdesk could not complete the request (HTTP %d)", status), summary))}
	case status == http.StatusNotFound:
		return freshdeskExchange{outcome: exchangeNotFound, response: response,
			failure: freshdeskFailure(sdkgo.FailureNotFound, operation, withErrorSummary("Freshdesk found no such resource (HTTP 404)", summary))}
	case status >= 300 && status < 400:
		return freshdeskExchange{outcome: exchangeRejected, response: response,
			failure: freshdeskFailure(sdkgo.FailureProtocol, operation, fmt.Sprintf("Freshdesk redirected the request (HTTP %d); check that the domain is the freshdesk.com helpdesk domain", status))}
	default:
		return freshdeskExchange{outcome: exchangeRejected, response: response,
			failure: freshdeskFailure(rejectionFailureKind(status), operation, withErrorSummary(fmt.Sprintf("Freshdesk rejected the request (HTTP %d)", status), summary))}
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
	case http.StatusBadRequest:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}

// describeFreshdeskError reads only code and field tokens; description and message are text and never read.
func describeFreshdeskError(body []byte, credentials Credentials) freshdeskErrorSummary {
	summary := freshdeskErrorSummary{}
	secret := credentials.APIKey.Reveal()
	if tokens := providerhttp.ReadErrorTokens(body, []string{"/code"}); len(tokens) == 1 && isCredentialFreeToken(tokens[0], secret) {
		summary.errorCode = tokens[0]
	}
	var document struct {
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(body, &document) != nil {
		return summary
	}
	var fieldErrors []string
	for index := range document.Errors {
		fields := providerhttp.ReadErrorTokens(body, []string{"/errors/" + strconv.Itoa(index) + "/field"})
		if len(fields) != 1 || !errorFieldPattern.MatchString(fields[0]) || !isCredentialFreeToken(fields[0], secret) {
			continue
		}
		detail := fields[0]
		codes := providerhttp.ReadErrorTokens(body, []string{"/errors/" + strconv.Itoa(index) + "/code"})
		if len(codes) == 1 && isCredentialFreeToken(codes[0], secret) {
			detail += "=" + codes[0]
		}
		fieldErrors = append(fieldErrors, detail)
	}
	sort.Strings(fieldErrors)
	summary.fieldErrors = fieldErrors[:min(len(fieldErrors), maximumReportedErrorDetails)]
	return summary
}

// isCredentialFreeToken rejects an error token that echoes the API key back.
func isCredentialFreeToken(token string, secret string) bool {
	return secret == "" || !strings.Contains(token, secret)
}

func withErrorSummary(message string, summary freshdeskErrorSummary) string {
	var parts []string
	if summary.errorCode != "" {
		parts = append(parts, summary.errorCode)
	}
	if len(summary.fieldErrors) != 0 {
		parts = append(parts, "errors: "+strings.Join(summary.fieldErrors, ", "))
	}
	if len(parts) == 0 {
		return message
	}
	return message + " [" + strings.Join(parts, "; ") + "]"
}

func (client *Client) receipt(call sdkgo.Call, response freshdeskResponse, objectID int64) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderRequestID: response.requestID, ObservedAt: client.now().UTC(),
	}
	if objectID > 0 {
		receipt.ProviderObjectID = strconv.FormatInt(objectID, 10)
	}
	return receipt
}

// isConnectionNeverEstablished reports a dial failure, after which Freshdesk cannot have received the request.
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

// hasNextPage reports whether a Freshdesk list response's Link header names a next page.
func hasNextPage(header http.Header) bool {
	for _, link := range header.Values("Link") {
		if strings.Contains(link, `rel="next"`) {
			return true
		}
	}
	return false
}

func ticketPath(ticketID int64) string {
	return "/tickets/" + strconv.FormatInt(ticketID, 10)
}

func validateResolvedCredentials(credentials Credentials) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) || strings.Contains(credentials.APIKey.Reveal(), ":") {
		return errors.New("Freshdesk API key must be printable ASCII without spaces or colons")
	}
	return nil
}

func freshdeskFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func freshdeskFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := freshdeskFailure(kind, operation, message)
	return &failure
}
