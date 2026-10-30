// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package support implements Zendesk Support ticket operations as Dex connector
// Steps: searchTickets and getTicket read tickets, createTicket creates one
// ticket under a Step-derived Zendesk Idempotency-Key, and updateTicket changes
// ticket fields and adds at most one comment per Step execution.
//
// The connector authenticates with a Zendesk API token as {email}/token over
// HTTP Basic and sends every request to https://{subdomain}.zendesk.com/api/v2.
// Status, priority, and type values are Zendesk's own enums, never remapped.
package support

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
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
	providerName = "zendesk"

	// defaultRequestTimeout keeps an updateTicket read, audit read, and write inside the 30-second Execute timeout.
	defaultRequestTimeout = 9 * time.Second

	idempotencyKeyHeader    = "Idempotency-Key"
	idempotencyLookupHeader = "X-Idempotency-Lookup"
	requestIDHeader         = "X-Zendesk-Request-Id"

	// idempotencyMetadataKey names the audit metadata entry that records which Step wrote a change.
	idempotencyMetadataKey = "dex_idempotency_key"

	maximumReportedErrorDetails = 5
)

var (
	subdomainPattern          = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	errorDetailFieldPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	errZendeskRequestNotBuilt = errors.New("Zendesk request could not be built")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	apiBaseURL string
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 9 seconds per request.
// The caller retains ownership of the client and its transport. The connector uses a copy
// that never follows redirects, so the API token is never replayed to another host.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAPIBaseURL replaces https://{subdomain}.zendesk.com/api/v2 for a local Zendesk-compatible
// fake. The URL must use HTTPS unless its host is loopback, and it must not carry user
// information, a query, or a fragment. Production connections leave it unset.
func WithAPIBaseURL(baseURL string) Option {
	return func(options *clientOptions) { options.apiBaseURL = baseURL }
}

// Client executes authenticated Zendesk Support API requests for connector operations.
// A Client is safe for concurrent use by several Steps.
type Client struct {
	apiBaseURL       string
	agentTicketURL   string
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
	now              func() time.Time
}

type zendeskRequest struct {
	method         string
	path           string
	query          url.Values
	payload        any
	idempotencyKey sdkgo.IdempotencyKey
}

type zendeskResponse struct {
	statusCode int
	header     http.Header
	body       []byte
	requestID  string
}

// exchangeOutcome is the provider-neutral meaning of one Zendesk request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeNotFound
	exchangeRetryable
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// zendeskExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type zendeskExchange struct {
	outcome    exchangeOutcome
	response   zendeskResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// zendeskErrorSummary holds only Zendesk's machine-readable error code and detail field names and types.
type zendeskErrorSummary struct {
	errorCode string
	details   []string
}

// New validates configuration and constructs a Zendesk Support client.
// Credentials are resolved before every provider request, so a replaced API token takes
// effect without a restart; the subdomain and response limit are startup configuration.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !subdomainPattern.MatchString(config.Subdomain) {
		return nil, errors.New("Zendesk subdomain must be lowercase letters, digits, and hyphens, such as acme for https://acme.zendesk.com")
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Zendesk response limit must be positive")
	}
	if credentials == nil {
		return nil, errors.New("Zendesk credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Zendesk connector option is nil")
		}
		option(&dependencies)
	}
	accountURL := "https://" + config.Subdomain + ".zendesk.com"
	apiBaseURL := accountURL + "/api/v2"
	if dependencies.apiBaseURL != "" {
		validated, err := providerhttp.ValidateBaseURL(dependencies.apiBaseURL)
		if err != nil {
			return nil, fmt.Errorf("Zendesk API base URL: %w", err)
		}
		apiBaseURL = validated
	}
	return &Client{
		apiBaseURL: apiBaseURL, agentTicketURL: accountURL + "/agent/tickets/",
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

// resolveCredentials returns a header-safe API token and bare agent address, or a safe defect Failure.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return Credentials{}, zendeskFailurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable")
	}
	return credentials, nil
}

// exchange sends one authenticated request and classifies its response; every operation's retry is duplicate-safe.
func (client *Client) exchange(call sdkgo.Call, credentials Credentials, operation string, request zendeskRequest) zendeskExchange {
	httpRequest, err := client.buildRequest(call, credentials, request)
	if err != nil {
		return zendeskExchange{outcome: exchangeDefect, failure: zendeskFailure(sdkgo.FailureLocalDefect, operation, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return zendeskExchange{outcome: exchangeRetryable, failure: zendeskFailure(sdkgo.FailureTransport, operation, "Zendesk request failed before a response arrived")}
	}
	body, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	closeErr := httpResponse.Body.Close()
	response := zendeskResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header, requestID: httpResponse.Header.Get(requestIDHeader)}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge) && isSuccess:
		return zendeskExchange{outcome: exchangeInvalid, response: response, failure: zendeskFailure(sdkgo.FailureResponseTooLarge, operation, "Zendesk response exceeds the configured maxResponseBytes limit")}
	case readErr != nil && !errors.Is(readErr, providerhttp.ErrBodyTooLarge), closeErr != nil:
		return zendeskExchange{outcome: exchangeRetryable, response: response, failure: zendeskFailure(sdkgo.FailureTransport, operation, "Zendesk response could not be read")}
	}
	response.body = body
	if isSuccess {
		if bytes.Contains(body, []byte(credentials.APIToken.Reveal())) {
			response.body = nil
			return zendeskExchange{outcome: exchangeInvalid, response: response, failure: zendeskFailure(sdkgo.FailureProtocol, operation, "Zendesk response reflected the connection credential")}
		}
		return zendeskExchange{outcome: exchangeSucceeded, response: response}
	}
	return client.classifyFailureStatus(response, credentials, operation)
}

func (client *Client) buildRequest(call sdkgo.Call, credentials Credentials, request zendeskRequest) (*http.Request, error) {
	var body *bytes.Reader
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return nil, errZendeskRequestNotBuilt
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
		return nil, errZendeskRequestNotBuilt
	}
	httpRequest.SetBasicAuth(credentials.Email+"/token", credentials.APIToken.Reveal())
	httpRequest.Header.Set("Accept", "application/json")
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	if request.idempotencyKey != "" {
		httpRequest.Header.Set(idempotencyKeyHeader, string(request.idempotencyKey))
	}
	return httpRequest, nil
}

// classifyFailureStatus maps a non-2xx response; Zendesk documents retrying a 409 collision after a fresh read.
func (client *Client) classifyFailureStatus(response zendeskResponse, credentials Credentials, operation string) zendeskExchange {
	summary := describeZendeskError(response.body, credentials)
	response.body = nil
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
	switch status := response.statusCode; {
	case status == http.StatusTooManyRequests:
		return zendeskExchange{outcome: exchangeRetryable, response: response, retryAfter: retryAfter,
			failure: zendeskFailure(sdkgo.FailureRateLimit, operation, withErrorSummary("Zendesk rate limited the request (HTTP 429)", summary))}
	case status == http.StatusRequestTimeout || status == http.StatusConflict || status >= 500:
		kind := sdkgo.FailureAvailability
		if status == http.StatusConflict {
			kind = sdkgo.FailureConflict
		}
		return zendeskExchange{outcome: exchangeRetryable, response: response, retryAfter: retryAfter,
			failure: zendeskFailure(kind, operation, withErrorSummary(fmt.Sprintf("Zendesk could not complete the request yet (HTTP %d)", status), summary))}
	case status == http.StatusNotFound || status == http.StatusGone:
		return zendeskExchange{outcome: exchangeNotFound, response: response,
			failure: zendeskFailure(sdkgo.FailureNotFound, operation, withErrorSummary(fmt.Sprintf("Zendesk found no such resource (HTTP %d)", status), summary))}
	case status >= 300 && status < 400:
		return zendeskExchange{outcome: exchangeRejected, response: response,
			failure: zendeskFailure(sdkgo.FailureProtocol, operation, fmt.Sprintf("Zendesk redirected the request (HTTP %d); check that the subdomain is the zendesk.com account subdomain", status))}
	default:
		return zendeskExchange{outcome: exchangeRejected, response: response,
			failure: zendeskFailure(rejectionFailureKind(status, summary), operation, withErrorSummary(fmt.Sprintf("Zendesk rejected the request (HTTP %d)", status), summary))}
	}
}

func rejectionFailureKind(status int, summary zendeskErrorSummary) sdkgo.FailureKind {
	switch {
	case summary.errorCode == "IdempotentRequestError":
		return sdkgo.FailureConflict
	case status == http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case status == http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}

// describeZendeskError reads only error and details tokens; descriptions are message text and never read.
func describeZendeskError(body []byte, credentials Credentials) zendeskErrorSummary {
	summary := zendeskErrorSummary{}
	secret := credentials.APIToken.Reveal()
	if tokens := providerhttp.ReadErrorTokens(body, []string{"/error"}); len(tokens) == 1 && isCredentialFreeToken(tokens[0], secret) {
		summary.errorCode = tokens[0]
	}
	var document struct {
		Details map[string]json.RawMessage `json:"details"`
	}
	if json.Unmarshal(body, &document) != nil {
		return summary
	}
	fields := make([]string, 0, len(document.Details))
	for field := range document.Details {
		if errorDetailFieldPattern.MatchString(field) && isCredentialFreeToken(field, secret) {
			fields = append(fields, field)
		}
	}
	sort.Strings(fields)
	for _, field := range fields[:min(len(fields), maximumReportedErrorDetails)] {
		detail := field
		tokens := providerhttp.ReadErrorTokens(body, []string{"/details/" + field + "/0/type", "/details/" + field + "/0/error"})
		if len(tokens) != 0 && isCredentialFreeToken(tokens[0], secret) {
			detail += "=" + tokens[0]
		}
		summary.details = append(summary.details, detail)
	}
	return summary
}

// isCredentialFreeToken rejects an error token that echoes the API token back.
func isCredentialFreeToken(token string, secret string) bool {
	return secret == "" || !strings.Contains(token, secret)
}

func withErrorSummary(message string, summary zendeskErrorSummary) string {
	var parts []string
	if summary.errorCode != "" {
		parts = append(parts, summary.errorCode)
	}
	if len(summary.details) != 0 {
		parts = append(parts, "details: "+strings.Join(summary.details, ", "))
	}
	if len(parts) == 0 {
		return message
	}
	return message + " [" + strings.Join(parts, "; ") + "]"
}

func (client *Client) receipt(call sdkgo.Call, response zendeskResponse, ticketID int64) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderRequestID: response.requestID, ObservedAt: client.now().UTC(),
	}
	if ticketID > 0 {
		receipt.ProviderObjectID = strconv.FormatInt(ticketID, 10)
	}
	return receipt
}

func ticketPath(ticketID int64) string {
	return "/tickets/" + strconv.FormatInt(ticketID, 10)
}

func validateResolvedCredentials(credentials Credentials) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	if !isBareEmailAddress(credentials.Email) {
		return errors.New("Zendesk agent email must be one bare address")
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.APIToken.Reveal()) {
		return errors.New("Zendesk API token must be printable ASCII without spaces")
	}
	return nil
}

func isBareEmailAddress(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value && !strings.ContainsAny(value, " \"'()<>,;")
}

func zendeskFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func zendeskFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := zendeskFailure(kind, operation, message)
	return &failure
}
