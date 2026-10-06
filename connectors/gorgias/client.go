// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package gorgias implements Gorgias helpdesk operations as Dex connector Steps:
// searchTickets, getTicket, and findCustomerByEmail read tickets and customers,
// updateTicket changes ticket fields and tags so that a repeated attempt writes
// nothing twice, and createTicket and addNote create a ticket or add one internal
// note or public email reply. Gorgias documents no idempotency key, so those two
// resend only after Gorgias provably did not apply a request and otherwise look the
// write up by the external ID they set.
//
// The connector authenticates with a Gorgias user's email address and API key over
// HTTP Basic and sends every request to https://{domain}.gorgias.com/api. Statuses
// and priorities are Gorgias's own strings, never remapped to another vocabulary.
package gorgias

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "gorgias"

	// defaultRequestTimeout keeps updateTicket's five requests inside its 45-second Execute timeout.
	defaultRequestTimeout = 8 * time.Second

	// requestIDHeader is not in Gorgias's API documentation, so the Receipt carries it only when present.
	requestIDHeader = "X-Request-Id"
	// apiCallLimitHeader reports requests made and allowed in the current rate-limit window, such as 10/40.
	apiCallLimitHeader = "X-Gorgias-Account-Api-Call-Limit"

	maximumReportedErrorFields = 5
)

var (
	domainPattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	errorFieldPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	// errorCodePattern accepts only snake_case identifiers such as ticket_merged, never a word of message text.
	errorCodePattern          = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+){1,7}$`)
	requestIDPattern          = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	apiCallLimitPattern       = regexp.MustCompile(`^[0-9]{1,6}/[0-9]{1,6}$`)
	errGorgiasRequestNotBuilt = errors.New("Gorgias request could not be built")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	apiBaseURL string
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 8 seconds per request.
// The caller retains ownership of the client and its transport. The connector uses a copy
// that never follows redirects, so the API key is never replayed to another host.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAPIBaseURL replaces https://{domain}.gorgias.com/api for a local Gorgias-compatible fake.
// The URL must use HTTPS unless its host is loopback, and it must not carry user information,
// a query, or a fragment. Production connections leave it unset.
func WithAPIBaseURL(baseURL string) Option {
	return func(options *clientOptions) { options.apiBaseURL = baseURL }
}

// Client executes authenticated Gorgias REST API requests for connector operations.
// A Client is safe for concurrent use by several Steps.
type Client struct {
	apiBaseURL       string
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
	now              func() time.Time
}

type gorgiasRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

type gorgiasResponse struct {
	statusCode   int
	header       http.Header
	body         []byte
	requestID    string
	apiCallLimit string
}

// exchangeOutcome is the provider-neutral meaning of one Gorgias request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeNotFound
	// exchangeRateLimited is a 429, which Gorgias returns instead of processing the request.
	exchangeRateLimited
	// exchangeNotSent is a connection that failed before any request byte reached Gorgias.
	exchangeNotSent
	// exchangeUnavailable is a 5xx, a 408, or a transport failure after connecting; a write may have been applied.
	exchangeUnavailable
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// gorgiasExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type gorgiasExchange struct {
	outcome    exchangeOutcome
	response   gorgiasResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// New validates configuration and constructs a Gorgias client.
// Credentials are resolved before every provider request, so a replaced API key takes
// effect without a restart; the domain and response limit are startup configuration.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !domainPattern.MatchString(config.Domain) {
		return nil, errors.New("Gorgias domain must be lowercase letters, digits, and hyphens, such as acme for https://acme.gorgias.com, without https://, .gorgias.com, or a path")
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Gorgias response limit must be positive")
	}
	if credentials == nil {
		return nil, errors.New("Gorgias credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Gorgias connector option is nil")
		}
		option(&dependencies)
	}
	apiBaseURL := "https://" + config.Domain + ".gorgias.com/api"
	if dependencies.apiBaseURL != "" {
		validated, err := providerhttp.ValidateBaseURL(dependencies.apiBaseURL)
		if err != nil {
			return nil, fmt.Errorf("Gorgias API base URL: %w", err)
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

// FindCustomerByEmail returns the findCustomerByEmail Query bound to this client.
func (client *Client) FindCustomerByEmail() FindCustomerByEmailOperation {
	return FindCustomerByEmailOperation{client: client}
}

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

// resolveCredentials returns header-safe credentials, or a safe defect Failure.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return Credentials{}, gorgiasFailurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable")
	}
	return credentials, nil
}

// exchange sends one authenticated request and classifies its response without deciding retry policy.
func (client *Client) exchange(call sdkgo.Call, credentials Credentials, operation string, request gorgiasRequest) gorgiasExchange {
	httpRequest, err := client.buildRequest(call, credentials, request)
	if err != nil {
		return gorgiasExchange{outcome: exchangeDefect, failure: gorgiasFailure(sdkgo.FailureLocalDefect, operation, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if isConnectionNeverEstablished(err) {
			return gorgiasExchange{outcome: exchangeNotSent, failure: gorgiasFailure(sdkgo.FailureTransport, operation, "Gorgias could not be reached; no request was sent")}
		}
		return gorgiasExchange{outcome: exchangeUnavailable, failure: gorgiasFailure(sdkgo.FailureTransport, operation, "Gorgias request failed before a response arrived")}
	}
	body, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	closeErr := httpResponse.Body.Close()
	response := gorgiasResponse{
		statusCode: httpResponse.StatusCode, header: httpResponse.Header,
		requestID:    safeHeaderValue(httpResponse.Header, requestIDHeader, requestIDPattern),
		apiCallLimit: safeHeaderValue(httpResponse.Header, apiCallLimitHeader, apiCallLimitPattern),
	}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge) && isSuccess:
		return gorgiasExchange{outcome: exchangeInvalid, response: response, failure: gorgiasFailure(sdkgo.FailureResponseTooLarge, operation, "Gorgias response exceeds the configured maxResponseBytes limit")}
	case readErr != nil && !errors.Is(readErr, providerhttp.ErrBodyTooLarge), closeErr != nil:
		return gorgiasExchange{outcome: exchangeUnavailable, response: response, failure: gorgiasFailure(sdkgo.FailureTransport, operation, "Gorgias response could not be read")}
	}
	response.body = body
	if isSuccess {
		if bytes.Contains(body, []byte(credentials.APIKey.Reveal())) {
			response.body = nil
			return gorgiasExchange{outcome: exchangeInvalid, response: response, failure: gorgiasFailure(sdkgo.FailureProtocol, operation, "Gorgias response reflected the connection credential")}
		}
		return gorgiasExchange{outcome: exchangeSucceeded, response: response}
	}
	return client.classifyFailureStatus(response, credentials, operation)
}

func (client *Client) buildRequest(call sdkgo.Call, credentials Credentials, request gorgiasRequest) (*http.Request, error) {
	target := client.apiBaseURL + request.path
	if len(request.query) != 0 {
		target += "?" + request.query.Encode()
	}
	var httpRequest *http.Request
	var err error
	if request.payload != nil {
		encoded, encodeErr := json.Marshal(request.payload)
		if encodeErr != nil {
			return nil, errGorgiasRequestNotBuilt
		}
		httpRequest, err = http.NewRequestWithContext(call.Context, request.method, target, bytes.NewReader(encoded))
	} else {
		httpRequest, err = http.NewRequestWithContext(call.Context, request.method, target, nil)
	}
	if err != nil {
		return nil, errGorgiasRequestNotBuilt
	}
	httpRequest.SetBasicAuth(credentials.Email, credentials.APIKey.Reveal())
	httpRequest.Header.Set("Accept", "application/json")
	if request.payload != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	return httpRequest, nil
}

// classifyFailureStatus maps a non-2xx response; Gorgias documents 429 as a request it did not perform.
func (client *Client) classifyFailureStatus(response gorgiasResponse, credentials Credentials, operation string) gorgiasExchange {
	summary := describeGorgiasError(response.body, credentials)
	response.body = nil
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
	switch status := response.statusCode; {
	case status == http.StatusTooManyRequests:
		return gorgiasExchange{outcome: exchangeRateLimited, response: response, retryAfter: retryAfter,
			failure: gorgiasFailure(sdkgo.FailureRateLimit, operation, withErrorSummary("Gorgias rate limited the request (HTTP 429)", summary))}
	case status == http.StatusRequestTimeout || status >= 500:
		return gorgiasExchange{outcome: exchangeUnavailable, response: response, retryAfter: retryAfter,
			failure: gorgiasFailure(sdkgo.FailureAvailability, operation, withErrorSummary(fmt.Sprintf("Gorgias could not complete the request (HTTP %d)", status), summary))}
	case status == http.StatusNotFound:
		return gorgiasExchange{outcome: exchangeNotFound, response: response,
			failure: gorgiasFailure(sdkgo.FailureNotFound, operation, withErrorSummary("Gorgias found no such resource (HTTP 404)", summary))}
	case status >= 300 && status < 400:
		return gorgiasExchange{outcome: exchangeRejected, response: response,
			failure: gorgiasFailure(sdkgo.FailureProtocol, operation, fmt.Sprintf("Gorgias redirected the request (HTTP %d); the ticket or customer may have been merged into another", status))}
	default:
		return gorgiasExchange{outcome: exchangeRejected, response: response,
			failure: gorgiasFailure(rejectionFailureKind(status), operation, withErrorSummary(fmt.Sprintf("Gorgias rejected the request (HTTP %d)", status), summary))}
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

// describeGorgiasError keeps only a snake_case error.msg code and the field names under error.data; text is never read.
func describeGorgiasError(body []byte, credentials Credentials) string {
	secret := credentials.APIKey.Reveal()
	var parts []string
	tokens := providerhttp.ReadErrorTokens(body, []string{"/error/msg"})
	if len(tokens) == 1 && errorCodePattern.MatchString(tokens[0]) && isCredentialFreeToken(tokens[0], secret) {
		parts = append(parts, tokens[0])
	}
	var document struct {
		Error struct {
			Data map[string]json.RawMessage `json:"data"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &document) != nil {
		return strings.Join(parts, "; ")
	}
	var fields []string
	for field := range document.Error.Data {
		if errorFieldPattern.MatchString(field) && isCredentialFreeToken(field, secret) {
			fields = append(fields, field)
		}
	}
	slices.Sort(fields)
	if len(fields) != 0 {
		parts = append(parts, "fields: "+strings.Join(fields[:min(len(fields), maximumReportedErrorFields)], ", "))
	}
	return strings.Join(parts, "; ")
}

// isCredentialFreeToken rejects an error token that echoes the API key back.
func isCredentialFreeToken(token string, secret string) bool {
	return secret == "" || !strings.Contains(token, secret)
}

func withErrorSummary(message string, summary string) string {
	if summary == "" {
		return message
	}
	return message + " [" + summary + "]"
}

func (client *Client) receipt(call sdkgo.Call, response gorgiasResponse, objectID int64) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderRequestID: response.requestID, ObservedAt: client.now().UTC(),
	}
	if objectID > 0 {
		receipt.ProviderObjectID = strconv.FormatInt(objectID, 10)
	}
	if response.apiCallLimit != "" {
		receipt.Metadata = map[string]string{"apiCallLimit": response.apiCallLimit}
	}
	return receipt
}

// isConnectionNeverEstablished reports a dial failure, after which Gorgias cannot have received the request.
func isConnectionNeverEstablished(err error) bool {
	var operationError *net.OpError
	return errors.As(err, &operationError) && operationError.Op == "dial"
}

func safeHeaderValue(header http.Header, name string, pattern *regexp.Regexp) string {
	value := header.Get(name)
	if pattern.MatchString(value) {
		return value
	}
	return ""
}

func ticketPath(ticketID int64) string {
	return "/tickets/" + strconv.FormatInt(ticketID, 10)
}

func validateResolvedCredentials(credentials Credentials) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	if !isBareEmailAddress(credentials.Email) || strings.Contains(credentials.Email, ":") {
		return errors.New("Gorgias email must be one bare email address")
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return errors.New("Gorgias API key must be printable ASCII without spaces")
	}
	return nil
}

func gorgiasFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func gorgiasFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := gorgiasFailure(kind, operation, message)
	return &failure
}
