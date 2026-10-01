// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package bamboohr implements BambooHR employee and time off operations as Dex connector
// Steps: getEmployee, findEmployeeByEmail, listEmployeeChanges, and listTimeOffRequests read
// BambooHR; updateEmployee sets plain employee fields so that a repeated attempt writes nothing
// twice; and addEmployee adds one employee, sending the request at most once per Step execution
// because BambooHR documents no idempotency key.
//
// The connector authenticates with a BambooHR user's API key over HTTP Basic, the key as the user
// name and x as the password, and sends every request to
// https://{companyDomain}.bamboohr.com/api/v1. Every request is permissioned as that user. Field
// names, employee statuses, change actions, and time off statuses are BambooHR's own, never
// remapped to another vocabulary.
package bamboohr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
	providerName = "bamboohr"

	// defaultRequestTimeout keeps updateEmployee's three requests inside the 30-second Execute timeout.
	defaultRequestTimeout = 9 * time.Second

	// basicAuthenticationPassword is the password BambooHR's curl example sends; any string is accepted.
	basicAuthenticationPassword = "x"

	// jsonContentType must be exact: BambooHR parses a body with any other Content-Type as XML.
	jsonContentType = "application/json"
)

var (
	companyDomainPattern       = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	errorCodePattern           = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)
	errBambooHRRequestNotBuilt = errors.New("BambooHR request could not be built")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	apiBaseURL string
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 9 seconds per request.
// The caller retains ownership of the client and its transport. The connector uses a copy that
// never follows redirects, so the API key is never replayed to another host.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAPIBaseURL replaces https://{companyDomain}.bamboohr.com/api/v1 for a local
// BambooHR-compatible fake. The URL must use HTTPS unless its host is loopback, and it must not
// carry user information, a query, or a fragment. Production connections leave it unset.
func WithAPIBaseURL(baseURL string) Option {
	return func(options *clientOptions) { options.apiBaseURL = baseURL }
}

// Client executes authenticated BambooHR API v1 requests for connector operations.
// A Client is safe for concurrent use by several Steps.
type Client struct {
	apiBaseURL       string
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
	now              func() time.Time
}

type bambooHRRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

type bambooHRResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

// exchangeOutcome is the provider-neutral meaning of one BambooHR request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeNotFound
	// exchangeRateLimited is a 429 with Retry-After, which BambooHR sends instead of processing the request.
	exchangeRateLimited
	// exchangeLimitExceeded is a 429 without Retry-After, BambooHR's documented employee limit.
	exchangeLimitExceeded
	// exchangeNotSent is a connection that failed before any request byte reached BambooHR.
	exchangeNotSent
	// exchangeUnavailable is a 5xx, a 408, or a transport failure after connecting; a write may have been applied.
	exchangeUnavailable
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// bambooHRExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type bambooHRExchange struct {
	outcome    exchangeOutcome
	response   bambooHRResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// New validates configuration and constructs a BambooHR client.
// Credentials are resolved before every provider request, so a replaced API key takes effect
// without a restart; the company domain and response limit are startup configuration.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !companyDomainPattern.MatchString(config.CompanyDomain) {
		return nil, errors.New("BambooHR companyDomain must be lowercase letters, digits, and hyphens, such as mycompany for https://mycompany.bamboohr.com")
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("BambooHR response limit must be positive")
	}
	if credentials == nil {
		return nil, errors.New("BambooHR credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("BambooHR connector option is nil")
		}
		option(&dependencies)
	}
	apiBaseURL := "https://" + config.CompanyDomain + ".bamboohr.com/api/v1"
	if dependencies.apiBaseURL != "" {
		validated, err := providerhttp.ValidateBaseURL(dependencies.apiBaseURL)
		if err != nil {
			return nil, fmt.Errorf("BambooHR API base URL: %w", err)
		}
		apiBaseURL = validated
	}
	return &Client{
		apiBaseURL:  apiBaseURL,
		httpClient:  providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials: credentials, maxResponseBytes: config.MaxResponseBytes, now: time.Now,
	}, nil
}

// GetEmployee returns the getEmployee Query bound to this client.
func (client *Client) GetEmployee() GetEmployeeOperation { return GetEmployeeOperation{client: client} }

// FindEmployeeByEmail returns the findEmployeeByEmail Query bound to this client.
func (client *Client) FindEmployeeByEmail() FindEmployeeByEmailOperation {
	return FindEmployeeByEmailOperation{client: client}
}

// ListEmployeeChanges returns the listEmployeeChanges Query bound to this client.
func (client *Client) ListEmployeeChanges() ListEmployeeChangesOperation {
	return ListEmployeeChangesOperation{client: client}
}

// ListTimeOffRequests returns the listTimeOffRequests Query bound to this client.
func (client *Client) ListTimeOffRequests() ListTimeOffRequestsOperation {
	return ListTimeOffRequestsOperation{client: client}
}

// UpdateEmployee returns the updateEmployee Mutation bound to this client.
func (client *Client) UpdateEmployee() UpdateEmployeeOperation {
	return UpdateEmployeeOperation{client: client}
}

// AddEmployee returns the addEmployee Mutation bound to this client.
func (client *Client) AddEmployee() AddEmployeeOperation { return AddEmployeeOperation{client: client} }

// resolveCredentials returns a header-safe API key, or a safe defect Failure.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || validateResolvedCredentials(credentials) != nil {
		return Credentials{}, bambooHRFailurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable")
	}
	return credentials, nil
}

// exchange sends one authenticated request and classifies its response without deciding retry policy.
func (client *Client) exchange(call sdkgo.Call, credentials Credentials, operation string, request bambooHRRequest) bambooHRExchange {
	httpRequest, err := client.buildRequest(call, credentials, request)
	if err != nil {
		return bambooHRExchange{outcome: exchangeDefect, failure: bambooHRFailure(sdkgo.FailureLocalDefect, operation, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if isConnectionNeverEstablished(err) {
			return bambooHRExchange{outcome: exchangeNotSent, failure: bambooHRFailure(sdkgo.FailureTransport, operation, "BambooHR could not be reached; no request was sent")}
		}
		return bambooHRExchange{outcome: exchangeUnavailable, failure: bambooHRFailure(sdkgo.FailureTransport, operation, "BambooHR request failed before a response arrived")}
	}
	body, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	closeErr := httpResponse.Body.Close()
	response := bambooHRResponse{statusCode: httpResponse.StatusCode, header: httpResponse.Header}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge) && isSuccess:
		return bambooHRExchange{outcome: exchangeInvalid, response: response, failure: bambooHRFailure(sdkgo.FailureResponseTooLarge, operation, "BambooHR response exceeds the configured maxResponseBytes limit")}
	case readErr != nil && !errors.Is(readErr, providerhttp.ErrBodyTooLarge), closeErr != nil:
		return bambooHRExchange{outcome: exchangeUnavailable, response: response, failure: bambooHRFailure(sdkgo.FailureTransport, operation, "BambooHR response could not be read")}
	}
	response.body = body
	if isSuccess {
		if bytes.Contains(body, []byte(credentials.APIKey.Reveal())) {
			response.body = nil
			return bambooHRExchange{outcome: exchangeInvalid, response: response, failure: bambooHRFailure(sdkgo.FailureProtocol, operation, "BambooHR response reflected the connection credential")}
		}
		return bambooHRExchange{outcome: exchangeSucceeded, response: response}
	}
	return client.classifyFailureStatus(response, credentials, operation)
}

func (client *Client) buildRequest(call sdkgo.Call, credentials Credentials, request bambooHRRequest) (*http.Request, error) {
	target := client.apiBaseURL + request.path
	if len(request.query) != 0 {
		target += "?" + request.query.Encode()
	}
	var httpRequest *http.Request
	var err error
	if request.payload != nil {
		encoded, encodeErr := json.Marshal(request.payload)
		if encodeErr != nil {
			return nil, errBambooHRRequestNotBuilt
		}
		httpRequest, err = http.NewRequestWithContext(call.Context, request.method, target, bytes.NewReader(encoded))
	} else {
		httpRequest, err = http.NewRequestWithContext(call.Context, request.method, target, nil)
	}
	if err != nil {
		return nil, errBambooHRRequestNotBuilt
	}
	// BambooHR recommends sending credentials on every request rather than answering its 401 challenge.
	httpRequest.SetBasicAuth(credentials.APIKey.Reveal(), basicAuthenticationPassword)
	httpRequest.Header.Set("Accept", jsonContentType)
	if request.payload != nil {
		httpRequest.Header.Set("Content-Type", jsonContentType)
	}
	return httpRequest, nil
}

// classifyFailureStatus maps a non-2xx response. BambooHR's X-BambooHR-Error-Message header is
// free text, so it is never read.
func (client *Client) classifyFailureStatus(response bambooHRResponse, credentials Credentials, operation string) bambooHRExchange {
	errorCode := describeBambooHRError(response.body, credentials)
	response.body = nil
	retryAfterHeader := response.header.Get("Retry-After")
	retryAfter := providerhttp.ParseRetryAfter(retryAfterHeader, client.now())
	switch status := response.statusCode; {
	case status == http.StatusTooManyRequests && retryAfterHeader != "":
		return bambooHRExchange{outcome: exchangeRateLimited, response: response, retryAfter: retryAfter,
			failure: bambooHRFailure(sdkgo.FailureRateLimit, operation, withErrorCode("BambooHR rate limited the request (HTTP 429)", errorCode))}
	case status == http.StatusTooManyRequests:
		return bambooHRExchange{outcome: exchangeLimitExceeded, response: response,
			failure: bambooHRFailure(sdkgo.FailureQuotaExhausted, operation, withErrorCode("BambooHR reported a limit exceeded without Retry-After (HTTP 429)", errorCode))}
	case status == http.StatusRequestTimeout || status >= 500:
		return bambooHRExchange{outcome: exchangeUnavailable, response: response, retryAfter: retryAfter,
			failure: bambooHRFailure(sdkgo.FailureAvailability, operation, withErrorCode(fmt.Sprintf("BambooHR could not complete the request (HTTP %d)", status), errorCode))}
	case status == http.StatusNotFound:
		return bambooHRExchange{outcome: exchangeNotFound, response: response,
			failure: bambooHRFailure(sdkgo.FailureNotFound, operation, withErrorCode("BambooHR found no such resource (HTTP 404)", errorCode))}
	case status >= 300 && status < 400:
		return bambooHRExchange{outcome: exchangeRejected, response: response,
			failure: bambooHRFailure(sdkgo.FailureProtocol, operation, fmt.Sprintf("BambooHR redirected the request (HTTP %d); check that companyDomain is the mycompany in https://mycompany.bamboohr.com", status))}
	default:
		return bambooHRExchange{outcome: exchangeRejected, response: response,
			failure: bambooHRFailure(rejectionFailureKind(status), operation, withErrorCode(fmt.Sprintf("BambooHR rejected the request (HTTP %d)", status), errorCode))}
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
	case http.StatusBadRequest, http.StatusNotAcceptable, http.StatusUnprocessableEntity:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}

// describeBambooHRError reads only a machine-readable error code; every message field is text and never read.
func describeBambooHRError(body []byte, credentials Credentials) string {
	secret := credentials.APIKey.Reveal()
	for _, pointer := range []string{"/error/code", "/code"} {
		tokens := providerhttp.ReadErrorTokens(body, []string{pointer})
		if len(tokens) == 1 && errorCodePattern.MatchString(tokens[0]) && (secret == "" || !strings.Contains(tokens[0], secret)) {
			return tokens[0]
		}
	}
	return ""
}

func withErrorCode(message string, errorCode string) string {
	if errorCode == "" {
		return message
	}
	return message + " [" + errorCode + "]"
}

func (client *Client) receipt(call sdkgo.Call, employeeID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: employeeID, ObservedAt: client.now().UTC(),
	}
}

// isConnectionNeverEstablished reports a dial failure, after which BambooHR cannot have received the request.
func isConnectionNeverEstablished(err error) bool {
	var operationError *net.OpError
	return errors.As(err, &operationError) && operationError.Op == "dial"
}

func validateResolvedCredentials(credentials Credentials) error {
	if err := credentials.Validate(); err != nil {
		return err
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) || strings.Contains(credentials.APIKey.Reveal(), ":") {
		return errors.New("BambooHR API key must be printable ASCII without spaces or colons")
	}
	return nil
}

func bambooHRFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func bambooHRFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := bambooHRFailure(kind, operation, message)
	return &failure
}
