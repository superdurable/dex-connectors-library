// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package mailchimp implements Mailchimp Marketing API audience and campaign operations as Dex
// connector Steps: getMember and listMembers read audience contacts, upsertMember and
// updateMemberTags write a contact so that a repeated attempt writes the same values, and
// sendCampaign sends an existing draft campaign at most once per Step execution, because a send is
// irreversible and Mailchimp documents no idempotency key.
//
// The connector authenticates with a Mailchimp API key sent as a Bearer token. A key ends with a
// hyphen and the account's data center, such as -us6, and the connector derives the API host
// https://us6.api.mailchimp.com/3.0 from it before every request, so a connection needs no server
// field. Mailchimp addresses a contact by the MD5 hash of the lowercased email address;
// SubscriberHash computes it.
package mailchimp

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
	providerName = "mailchimp"

	// defaultRequestTimeout keeps one request inside the 30-second Execute timeout of an async operation.
	defaultRequestTimeout = 25 * time.Second

	// requestIDHeader is the header Mailchimp's error documentation shows on its responses.
	requestIDHeader = "X-Request-Id"

	maximumReportedErrorFields = 5
)

var (
	// dataCenterPattern matches the suffix Mailchimp appends to an API key, such as us6 or us21.
	dataCenterPattern = regexp.MustCompile(`^[a-z]{2,10}[0-9]{1,4}$`)
	apiKeyBodyPattern = regexp.MustCompile(`^[A-Za-z0-9]{8,128}$`)
	// resourceIDPattern bounds audience and campaign IDs to one safe URL path segment.
	resourceIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)
	errorTitlePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z ]{0,78}[A-Za-z]$`)
	errorFieldPattern = regexp.MustCompile(`^[A-Za-z0-9_.\[\]-]{1,64}$`)
	requestIDPattern  = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

	errMailchimpRequestNotBuilt = errors.New("Mailchimp request could not be built")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	apiBaseURL string
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 25 seconds per request.
// The caller retains ownership of the client and its transport. The connector uses a copy that
// never follows redirects, so the API key is never replayed to another host.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAPIBaseURL replaces the https://{dc}.api.mailchimp.com/3.0 host derived from the API key with
// a local Mailchimp-compatible fake. The URL must use HTTPS unless its host is loopback, and it must
// not carry user information, a query, or a fragment. Production connections leave it unset.
func WithAPIBaseURL(baseURL string) Option {
	return func(options *clientOptions) { options.apiBaseURL = baseURL }
}

// Client executes authenticated Mailchimp Marketing API 3.0 requests for connector operations.
// A Client is safe for concurrent use by several Steps.
type Client struct {
	apiBaseURL       string
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
	now              func() time.Time
}

type mailchimpRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

type mailchimpResponse struct {
	statusCode int
	header     http.Header
	body       []byte
	requestID  string
}

// exchangeOutcome is the provider-neutral meaning of one Mailchimp request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeNotFound
	// exchangeRateLimited is a 429 or a document-less 403, which Mailchimp documents as throttling.
	exchangeRateLimited
	// exchangeNotSent is a connection that failed before any request byte reached Mailchimp.
	exchangeNotSent
	// exchangeUnavailable is a 5xx, a 408, or a transport failure after connecting; a write may have been applied.
	exchangeUnavailable
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// mailchimpExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type mailchimpExchange struct {
	outcome    exchangeOutcome
	response   mailchimpResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// mailchimpErrorSummary holds only the problem title and field names; detail and messages are text and never read.
type mailchimpErrorSummary struct {
	title  string
	fields []string
}

// resolvedConnection is a validated API key and the API base URL it selects.
type resolvedConnection struct {
	credentials Credentials
	apiBaseURL  string
}

// New validates configuration and constructs a Mailchimp client.
// Credentials are resolved before every provider request, so a replaced API key, including one
// for another data center, takes effect without a restart; the response limit is startup configuration.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Mailchimp response limit must be positive")
	}
	if credentials == nil {
		return nil, errors.New("Mailchimp credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Mailchimp connector option is nil")
		}
		option(&dependencies)
	}
	apiBaseURL := ""
	if dependencies.apiBaseURL != "" {
		validated, err := providerhttp.ValidateBaseURL(dependencies.apiBaseURL)
		if err != nil {
			return nil, fmt.Errorf("Mailchimp API base URL: %w", err)
		}
		apiBaseURL = validated
	}
	return &Client{
		apiBaseURL: apiBaseURL, httpClient: providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials: credentials, maxResponseBytes: config.MaxResponseBytes, now: time.Now,
	}, nil
}

// GetMember returns the getMember Query bound to this client.
func (client *Client) GetMember() GetMemberOperation { return GetMemberOperation{client: client} }

// ListMembers returns the listMembers Query bound to this client.
func (client *Client) ListMembers() ListMembersOperation { return ListMembersOperation{client: client} }

// UpsertMember returns the upsertMember Mutation bound to this client.
func (client *Client) UpsertMember() UpsertMemberOperation {
	return UpsertMemberOperation{client: client}
}

// UpdateMemberTags returns the updateMemberTags Mutation bound to this client.
func (client *Client) UpdateMemberTags() UpdateMemberTagsOperation {
	return UpdateMemberTagsOperation{client: client}
}

// SendCampaign returns the sendCampaign Mutation bound to this client.
func (client *Client) SendCampaign() SendCampaignOperation {
	return SendCampaignOperation{client: client}
}

// resolveConnection returns a header-safe API key and its API base URL, or a safe defect Failure.
func (client *Client) resolveConnection(call sdkgo.Call, operation string) (resolvedConnection, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return resolvedConnection{}, mailchimpFailurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable")
	}
	dataCenter, err := validateResolvedCredentials(credentials)
	if err != nil {
		return resolvedConnection{}, mailchimpFailurePointer(sdkgo.FailureAuthentication, operation, err.Error())
	}
	apiBaseURL := client.apiBaseURL
	if apiBaseURL == "" {
		apiBaseURL = "https://" + dataCenter + ".api.mailchimp.com/3.0"
	}
	return resolvedConnection{credentials: credentials, apiBaseURL: apiBaseURL}, nil
}

// exchange sends one authenticated request and classifies its response without deciding retry policy.
func (client *Client) exchange(call sdkgo.Call, connection resolvedConnection, operation string, request mailchimpRequest) mailchimpExchange {
	httpRequest, err := client.buildRequest(call, connection, request)
	if err != nil {
		return mailchimpExchange{outcome: exchangeDefect, failure: mailchimpFailure(sdkgo.FailureLocalDefect, operation, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if isConnectionNeverEstablished(err) {
			return mailchimpExchange{outcome: exchangeNotSent, failure: mailchimpFailure(sdkgo.FailureTransport, operation, "Mailchimp could not be reached; no request was sent")}
		}
		return mailchimpExchange{outcome: exchangeUnavailable, failure: mailchimpFailure(sdkgo.FailureTransport, operation, "Mailchimp request failed before a response arrived")}
	}
	body, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	closeErr := httpResponse.Body.Close()
	response := mailchimpResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header, requestID: safeRequestID(httpResponse.Header)}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge) && isSuccess:
		return mailchimpExchange{outcome: exchangeInvalid, response: response, failure: mailchimpFailure(sdkgo.FailureResponseTooLarge, operation, "Mailchimp response exceeds the configured maxResponseBytes limit")}
	case readErr != nil && !errors.Is(readErr, providerhttp.ErrBodyTooLarge), closeErr != nil:
		return mailchimpExchange{outcome: exchangeUnavailable, response: response, failure: mailchimpFailure(sdkgo.FailureTransport, operation, "Mailchimp response could not be read")}
	}
	response.body = body
	if isSuccess {
		if bytes.Contains(body, []byte(connection.credentials.APIKey.Reveal())) {
			response.body = nil
			return mailchimpExchange{outcome: exchangeInvalid, response: response, failure: mailchimpFailure(sdkgo.FailureProtocol, operation, "Mailchimp response reflected the connection credential")}
		}
		return mailchimpExchange{outcome: exchangeSucceeded, response: response}
	}
	return client.classifyFailureStatus(response, connection.credentials, operation)
}

func (client *Client) buildRequest(call sdkgo.Call, connection resolvedConnection, request mailchimpRequest) (*http.Request, error) {
	target := connection.apiBaseURL + request.path
	if len(request.query) != 0 {
		target += "?" + request.query.Encode()
	}
	var httpRequest *http.Request
	var err error
	if request.payload != nil {
		encoded, encodeErr := json.Marshal(request.payload)
		if encodeErr != nil {
			return nil, errMailchimpRequestNotBuilt
		}
		httpRequest, err = http.NewRequestWithContext(call.Context, request.method, target, bytes.NewReader(encoded))
	} else {
		httpRequest, err = http.NewRequestWithContext(call.Context, request.method, target, nil)
	}
	if err != nil {
		return nil, errMailchimpRequestNotBuilt
	}
	httpRequest.Header.Set("Authorization", "Bearer "+connection.credentials.APIKey.Reveal())
	httpRequest.Header.Set("Accept", "application/json")
	if request.payload != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	return httpRequest, nil
}

// classifyFailureStatus maps a non-2xx response; a document-less 403 is Mailchimp's high-volume throttling.
func (client *Client) classifyFailureStatus(response mailchimpResponse, credentials Credentials, operation string) mailchimpExchange {
	summary, hasProblemDocument := describeMailchimpError(response.body, credentials)
	response.body = nil
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
	switch status := response.statusCode; {
	case status == http.StatusTooManyRequests:
		return mailchimpExchange{outcome: exchangeRateLimited, response: response, retryAfter: retryAfter,
			failure: mailchimpFailure(sdkgo.FailureRateLimit, operation, withErrorSummary("Mailchimp throttled the request (HTTP 429)", summary))}
	case status == http.StatusForbidden && !hasProblemDocument:
		return mailchimpExchange{outcome: exchangeRateLimited, response: response, retryAfter: retryAfter,
			failure: mailchimpFailure(sdkgo.FailureRateLimit, operation, "Mailchimp throttled the request (HTTP 403 without an error document)")}
	case status == http.StatusRequestTimeout || status >= 500:
		return mailchimpExchange{outcome: exchangeUnavailable, response: response, retryAfter: retryAfter,
			failure: mailchimpFailure(sdkgo.FailureAvailability, operation, withErrorSummary(fmt.Sprintf("Mailchimp could not complete the request (HTTP %d)", status), summary))}
	case status == http.StatusNotFound:
		return mailchimpExchange{outcome: exchangeNotFound, response: response,
			failure: mailchimpFailure(sdkgo.FailureNotFound, operation, withErrorSummary("Mailchimp found no such resource (HTTP 404)", summary))}
	case status >= 300 && status < 400:
		return mailchimpExchange{outcome: exchangeRejected, response: response,
			failure: mailchimpFailure(sdkgo.FailureProtocol, operation, fmt.Sprintf("Mailchimp redirected the request (HTTP %d); redirects are never followed", status))}
	default:
		return mailchimpExchange{outcome: exchangeRejected, response: response,
			failure: mailchimpFailure(rejectionFailureKind(status), operation, withErrorSummary(fmt.Sprintf("Mailchimp rejected the request (HTTP %d)", status), summary))}
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

// describeMailchimpError reads only title and field names; detail and messages can repeat contact data.
func describeMailchimpError(body []byte, credentials Credentials) (mailchimpErrorSummary, bool) {
	var document struct {
		Title  *string           `json:"title"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(body, &document) != nil || document.Title == nil {
		return mailchimpErrorSummary{}, false
	}
	summary := mailchimpErrorSummary{}
	secret := credentials.APIKey.Reveal()
	if errorTitlePattern.MatchString(*document.Title) && isCredentialFreeToken(*document.Title, secret) {
		summary.title = *document.Title
	}
	var fields []string
	for index := range document.Errors {
		tokens := providerhttp.ReadErrorTokens(body, []string{"/errors/" + strconv.Itoa(index) + "/field"})
		if len(tokens) == 1 && errorFieldPattern.MatchString(tokens[0]) && isCredentialFreeToken(tokens[0], secret) {
			fields = append(fields, tokens[0])
		}
	}
	sort.Strings(fields)
	summary.fields = fields[:min(len(fields), maximumReportedErrorFields)]
	return summary, true
}

// isCredentialFreeToken rejects an error token that echoes the API key back.
func isCredentialFreeToken(token string, secret string) bool {
	return secret == "" || !strings.Contains(token, secret)
}

func withErrorSummary(message string, summary mailchimpErrorSummary) string {
	var parts []string
	if summary.title != "" {
		parts = append(parts, summary.title)
	}
	if len(summary.fields) != 0 {
		parts = append(parts, "fields: "+strings.Join(summary.fields, ", "))
	}
	if len(parts) == 0 {
		return message
	}
	return message + " [" + strings.Join(parts, "; ") + "]"
}

func (client *Client) receipt(call sdkgo.Call, response mailchimpResponse, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ProviderRequestID: response.requestID, ObservedAt: client.now().UTC(),
	}
}

// isConnectionNeverEstablished reports a dial failure, after which Mailchimp cannot have received the request.
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

// validateResolvedCredentials checks the API key and returns the data center its suffix names.
func validateResolvedCredentials(credentials Credentials) (string, error) {
	if err := credentials.Validate(); err != nil {
		return "", err
	}
	apiKey := credentials.APIKey.Reveal()
	if !providerhttp.IsHeaderSafeCredential(apiKey) {
		return "", errors.New("Mailchimp API key must be printable ASCII without spaces")
	}
	separator := strings.LastIndexByte(apiKey, '-')
	if separator < 0 || !apiKeyBodyPattern.MatchString(apiKey[:separator]) || !dataCenterPattern.MatchString(apiKey[separator+1:]) {
		return "", errors.New("Mailchimp API key must end with a hyphen and its data center, such as -us6; paste the whole key Mailchimp showed")
	}
	return apiKey[separator+1:], nil
}

func validateResourceID(name string, value string) error {
	if !resourceIDPattern.MatchString(value) {
		return fmt.Errorf("%s must be a Mailchimp ID of 1 to 64 letters and digits", name)
	}
	return nil
}

func mailchimpFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func mailchimpFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := mailchimpFailure(kind, operation, message)
	return &failure
}
