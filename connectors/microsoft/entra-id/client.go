// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package entraid implements Microsoft Entra ID user-account and
// group-membership operations through Microsoft Graph as Dex connector Steps:
// read and list accounts, create one without duplicating it, disable and
// enable it, revoke its sign-in sessions, and add it to or remove it from a
// group.
//
// Applications use the generated operation-specific Step factories, such as
// NewCreateUserStep and NewAddUserToGroupStep, with a Connection built by
// NewLocalConnection or NewConnection. The app-only method acts with the
// Microsoft Graph application permissions an administrator granted to the
// app registration; the Microsoft OAuth method acts with the directory roles
// of the administrator who consented. The runnable example in
// examples/account-lifecycle onboards, reinstates, and offboards one account.
package entraid

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName          = "microsoft-entra-id"
	graphHost             = "graph.microsoft.com"
	graphBaseURL          = "https://" + graphHost + "/v1.0"
	defaultRequestTimeout = 25 * time.Second
	// maxListUsersPageSize is the documented Microsoft Graph users page maximum.
	maxListUsersPageSize = 999
	// propagationWindow tolerates Microsoft Entra replication delay after a write.
	propagationWindow = time.Minute
	// directoryConcurrencyViolationCode is the 409 code Microsoft documents as safe to repeat after a delay.
	directoryConcurrencyViolationCode = "Directory_ConcurrencyViolation"
)

// graphErrorCodePointers locate Microsoft Graph's machine-readable error codes; message text is never read.
var graphErrorCodePointers = []string{"/error/code", "/error/innerError/code", "/error/innererror/code"}

// reportableGraphErrorCodes are Microsoft Graph error codes that are safe to repeat in a Failure message.
var reportableGraphErrorCodes = map[string]bool{
	"Request_BadRequest": true, "Request_ResourceNotFound": true, "Request_UnsupportedQuery": true,
	"Authorization_RequestDenied": true, "Authorization_IdentityNotFound": true, "InvalidAuthenticationToken": true,
	"Directory_QuotaExceeded": true, directoryConcurrencyViolationCode: true, "Directory_ResultSizeLimitExceeded": true,
	"accessDenied": true, "badRequest": true, "invalidRequest": true, "itemNotFound": true, "unauthenticated": true,
	"serviceNotAvailable": true, "generalException": true, "activityLimitReached": true, "TooManyRequests": true,
}

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient       *http.Client
	localProviderURL string
}

// WithHTTPClient overrides the default 25-second HTTP client used for Microsoft
// Graph and token requests. The connector uses a copy that never follows
// redirects; the caller retains ownership of the original client and its transport.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithLocalProviderURL sends every request for https://graph.microsoft.com and
// https://login.microsoftonline.com to one loopback base URL without a path,
// such as http://127.0.0.1:8930, keeping each request's path and query. It is
// for local verification against a Microsoft-compatible fake only: New rejects
// a non-loopback URL, and the routed client refuses every other host.
func WithLocalProviderURL(baseURL string) Option {
	return func(options *clientOptions) { options.localProviderURL = baseURL }
}

// Client executes authenticated Microsoft Graph requests for connector operations.
// A Client is immutable after New and safe for concurrent use by Dex Workers.
type Client struct {
	httpClient           *http.Client
	credentials          sdkgo.CredentialProvider[Credentials]
	refreshDriver        sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes     int64
	listUsersPageSize    int
	creationKeyAttribute CreationKeyAttribute
	now                  func() time.Time
}

// graphRequest is one Microsoft Graph request; its encoded payload is resent once after a 401 forces a refresh.
type graphRequest struct {
	method string
	// path is appended to the v1.0 base URL; nextPageURL, when set, is a validated absolute nextLink instead.
	path                    string
	nextPageURL             string
	query                   url.Values
	payload                 any
	usesEventualConsistency bool
}

type graphResponse struct {
	status    int
	header    http.Header
	body      []byte
	requestID string
}

// graphRequestError is a safe local classification of a request that produced no usable response.
type graphRequestError struct {
	kind    sdkgo.FailureKind
	message string
}

// Error returns the safe human-readable failure message.
func (failure *graphRequestError) Error() string { return failure.message }

// exchangeOutcome is the provider-neutral meaning of one Microsoft Graph exchange.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeRetry
	exchangeNotFound
	exchangeBadRequest
	exchangeRejected
	exchangeInvalidResponse
	exchangeDefect
)

// graphExchange is one classified exchange that each operation maps to its own branch or Retry.
type graphExchange struct {
	outcome    exchangeOutcome
	response   graphResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// failureBranches names each operation's branch for a non-retried failure; a 400 maps to rejected.
type failureBranches struct {
	notFound        sdkgo.BranchID
	rejected        sdkgo.BranchID
	invalidResponse sdkgo.BranchID
	defect          sdkgo.BranchID
}

func (branches failureBranches) branchFor(outcome exchangeOutcome) sdkgo.BranchID {
	switch outcome {
	case exchangeNotFound:
		return branches.notFound
	case exchangeBadRequest, exchangeRejected:
		return branches.rejected
	case exchangeInvalidResponse:
		return branches.invalidResponse
	default:
		return branches.defect
	}
}

// New validates configuration and constructs an authenticated Microsoft Graph client.
// It fails when a limit is outside its documented range, the creation key
// attribute is unknown, a local provider URL is not loopback, or credentials is nil.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, errors.New("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Microsoft Entra ID connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.httpClient == nil {
		dependencies.httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	if dependencies.localProviderURL != "" {
		routed, err := newLocalProviderHTTPClient(dependencies.httpClient, dependencies.localProviderURL)
		if err != nil {
			return nil, err
		}
		dependencies.httpClient = routed
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Microsoft Entra ID response limit must be positive")
	}
	if config.ListUsersPageSize < 1 || config.ListUsersPageSize > maxListUsersPageSize {
		return nil, fmt.Errorf("Microsoft Entra ID listUsers page size must be from 1 to %d", maxListUsersPageSize)
	}
	return &Client{
		httpClient:           providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials:          credentials,
		refreshDriver:        NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes:     config.MaxResponseBytes,
		listUsersPageSize:    int(config.ListUsersPageSize),
		creationKeyAttribute: config.CreationKeyAttribute,
		now:                  time.Now,
	}, nil
}

// GetUser returns the getUser Query bound to this client.
func (client *Client) GetUser() GetUserOperation { return GetUserOperation{client: client} }

// ListUsers returns the listUsers Query bound to this client.
func (client *Client) ListUsers() ListUsersOperation { return ListUsersOperation{client: client} }

// CreateUser returns the createUser Mutation bound to this client.
func (client *Client) CreateUser() CreateUserOperation { return CreateUserOperation{client: client} }

// DisableUser returns the disableUser Mutation bound to this client.
func (client *Client) DisableUser() DisableUserOperation { return DisableUserOperation{client: client} }

// EnableUser returns the enableUser Mutation bound to this client.
func (client *Client) EnableUser() EnableUserOperation { return EnableUserOperation{client: client} }

// RevokeSignInSessions returns the revokeSignInSessions Mutation bound to this client.
func (client *Client) RevokeSignInSessions() RevokeSignInSessionsOperation {
	return RevokeSignInSessionsOperation{client: client}
}

// AddUserToGroup returns the addUserToGroup Mutation bound to this client.
func (client *Client) AddUserToGroup() AddUserToGroupOperation {
	return AddUserToGroupOperation{client: client}
}

// RemoveUserFromGroup returns the removeUserFromGroup Mutation bound to this client.
func (client *Client) RemoveUserFromGroup() RemoveUserFromGroupOperation {
	return RemoveUserFromGroupOperation{client: client}
}

func (client *Client) resolveCredential(call sdkgo.Call, operationID string) (Credentials, *sdkgo.Failure) {
	credential, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credential) != nil {
		return Credentials{}, failurePointer(sdkgo.FailureAuthentication, operationID, "connection credentials are unavailable")
	}
	return credential, nil
}

// readUser performs one user read with the bounded property selection.
func (client *Client) readUser(call sdkgo.Call, credential *Credentials, operationID string, userKey string, selectedProperties string) graphExchange {
	return client.exchange(call, credential, operationID, graphRequest{
		method: http.MethodGet, path: userPath(userKey), query: url.Values{"$select": {selectedProperties}},
	})
}

// readGroup checks that a group exists without reading its members.
func (client *Client) readGroup(call sdkgo.Call, credential *Credentials, operationID string, groupID string) graphExchange {
	return client.exchange(call, credential, operationID, graphRequest{
		method: http.MethodGet, path: groupPath(groupID), query: url.Values{"$select": {"id"}},
	})
}

// isWithinPropagationWindow reports whether the Step's first attempt started less than propagationWindow ago.
func (client *Client) isWithinPropagationWindow(call sdkgo.Call) bool {
	if call.Context == nil {
		return false
	}
	firstAttemptAt := call.Context.FirstAttemptAt()
	return !firstAttemptAt.IsZero() && client.now().Sub(firstAttemptAt) < propagationWindow
}

// invalidResponseExchange reclassifies a successful exchange whose body is unusable.
func (client *Client) invalidResponseExchange(operationID string, response graphResponse, message string) graphExchange {
	return graphExchange{outcome: exchangeInvalidResponse, response: response, failure: graphFailure(sdkgo.FailureProtocol, operationID, message)}
}

// exchange sends one request and classifies the response without choosing an operation branch.
func (client *Client) exchange(call sdkgo.Call, credential *Credentials, operationID string, request graphRequest) graphExchange {
	response, err := client.send(call, credential, request)
	var requestErr *graphRequestError
	if errors.As(err, &requestErr) {
		switch requestErr.kind {
		case sdkgo.FailureLocalDefect:
			return graphExchange{outcome: exchangeDefect, failure: graphFailure(requestErr.kind, operationID, requestErr.message)}
		case sdkgo.FailureResponseTooLarge:
			return graphExchange{outcome: exchangeInvalidResponse, response: response, failure: graphFailure(requestErr.kind, operationID, requestErr.message)}
		}
	}
	if err != nil {
		return graphExchange{outcome: exchangeRetry, response: response, failure: graphFailure(sdkgo.FailureTransport, operationID, "provider is unavailable")}
	}
	if isSuccessStatus(response.status) {
		return graphExchange{outcome: exchangeSucceeded, response: response}
	}
	return client.classifyFailureStatus(operationID, response)
}

// classifyFailureStatus maps a non-2xx response from its status and documented error code only.
func (client *Client) classifyFailureStatus(operationID string, response graphResponse) graphExchange {
	code := firstGraphErrorCode(response.body)
	switch {
	case response.status == http.StatusTooManyRequests:
		return client.retryExchange(operationID, response, sdkgo.FailureRateLimit, describeCode("provider throttled the request", code))
	case response.status == http.StatusRequestTimeout || (response.status >= 500 && response.status != http.StatusNotImplemented):
		return client.retryExchange(operationID, response, sdkgo.FailureAvailability, describeCode("provider is temporarily unavailable", code))
	case response.status == http.StatusConflict && code == directoryConcurrencyViolationCode:
		return client.retryExchange(operationID, response, sdkgo.FailureAvailability, describeCode("provider reported a concurrent directory change", code))
	case response.status == http.StatusNotFound:
		return graphExchange{outcome: exchangeNotFound, response: response, failure: graphFailure(sdkgo.FailureNotFound, operationID, describeCode("resource was not found", code))}
	case response.status == http.StatusBadRequest:
		return graphExchange{outcome: exchangeBadRequest, response: response, failure: graphFailure(sdkgo.FailureValidation, operationID, describeCode("provider rejected the request with HTTP 400", code))}
	default:
		message := describeCode(fmt.Sprintf("provider rejected the request with HTTP %d", response.status), code)
		return graphExchange{outcome: exchangeRejected, response: response, failure: graphFailure(statusFailureKind(response.status), operationID, message)}
	}
}

// retryExchange asks Dex to retry after Microsoft's Retry-After delay, capped at one hour.
func (client *Client) retryExchange(operationID string, response graphResponse, kind sdkgo.FailureKind, message string) graphExchange {
	retryAfter := time.Duration(0)
	if response.header != nil {
		retryAfter = providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
	}
	return graphExchange{outcome: exchangeRetry, response: response, failure: graphFailure(kind, operationID, message), retryAfter: retryAfter}
}

// send issues one authenticated request. After a 401 it forces one coordinated refresh, updates credential, and sends once more.
func (client *Client) send(call sdkgo.Call, credential *Credentials, request graphRequest) (graphResponse, error) {
	var encodedPayload []byte
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return graphResponse{}, &graphRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be encoded"}
		}
		encodedPayload = encoded
	}
	target := request.nextPageURL
	if target == "" {
		target = graphBaseURL + request.path
		if len(request.query) > 0 {
			target += "?" + encodeODataQuery(request.query)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		var body io.Reader
		if encodedPayload != nil {
			body = bytes.NewReader(encodedPayload)
		}
		httpRequest, err := http.NewRequestWithContext(call.Context, request.method, target, body)
		if err != nil {
			return graphResponse{}, &graphRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be built"}
		}
		httpRequest.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
		httpRequest.Header.Set("Accept", "application/json")
		if encodedPayload != nil || request.method == http.MethodPost {
			httpRequest.Header.Set("Content-Type", "application/json")
		}
		if request.usesEventualConsistency {
			httpRequest.Header.Set("ConsistencyLevel", "eventual")
		}
		httpResponse, err := client.httpClient.Do(httpRequest)
		if err != nil {
			return graphResponse{}, &graphRequestError{kind: sdkgo.FailureTransport, message: "provider request failed"}
		}
		// Error bodies get their own bound, so a small success limit never hides an error status.
		content, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, max(client.maxResponseBytes, providerhttp.MaxErrorBodyBytes))
		closeErr := httpResponse.Body.Close()
		response := graphResponse{status: httpResponse.StatusCode, header: httpResponse.Header, body: content, requestID: graphRequestID(httpResponse.Header)}
		isSuccess := isSuccessStatus(response.status)
		isOversized := errors.Is(readErr, providerhttp.ErrBodyTooLarge) || (isSuccess && int64(len(content)) > client.maxResponseBytes)
		if isOversized && isSuccess {
			response.body = nil
			return response, &graphRequestError{kind: sdkgo.FailureResponseTooLarge, message: "provider response exceeds the configured limit"}
		}
		if isOversized {
			// An oversized error body carries no usable code, so the status alone classifies it.
			response.body = nil
			readErr = nil
		}
		if readErr != nil || closeErr != nil {
			return response, &graphRequestError{kind: sdkgo.FailureTransport, message: "provider response could not be read"}
		}
		if response.status != http.StatusUnauthorized || attempt != 0 {
			return response, nil
		}
		if _, ok := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !ok {
			return response, nil
		}
		refreshed, err := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		if err != nil || validateResolvedCredentials(refreshed) != nil {
			return response, nil
		}
		*credential = refreshed
	}
	return graphResponse{}, &graphRequestError{kind: sdkgo.FailureLocalDefect, message: "authenticated request retry was exhausted"}
}

func (client *Client) receipt(call sdkgo.Call, response graphResponse, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ProviderRequestID: response.requestID, ObservedAt: client.now().UTC(),
	}
}

func validateResolvedCredentials(credentials Credentials) error {
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Microsoft Entra ID access token is missing or invalid")
	}
	switch credentials.AuthMethodID {
	case "", EntraAppOnlyAuthMethodID, MicrosoftOAuthAuthMethodID:
		return nil
	default:
		return errors.New("Microsoft Entra ID authorization method is invalid")
	}
}

// encodeODataQuery encodes query parameters in key order. Keys are connector
// constants such as $select, written literally; values are percent-encoded.
func encodeODataQuery(query url.Values) string {
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parameters := make([]string, 0, len(keys))
	for _, key := range keys {
		for _, value := range query[key] {
			parameters = append(parameters, key+"="+strings.ReplaceAll(url.QueryEscape(value), "+", "%20"))
		}
	}
	return strings.Join(parameters, "&")
}

// firstGraphErrorCode returns the most specific documented error code, never message text.
func firstGraphErrorCode(body []byte) string {
	codes := providerhttp.ReadErrorTokens(body, graphErrorCodePointers)
	if len(codes) == 0 {
		return ""
	}
	return codes[0]
}

func describeCode(message string, code string) string {
	if !reportableGraphErrorCodes[code] {
		return message
	}
	return message + " (" + code + ")"
}

func isSuccessStatus(status int) bool { return status >= 200 && status < 300 }

func statusFailureKind(status int) sdkgo.FailureKind {
	switch status {
	case http.StatusBadRequest:
		return sdkgo.FailureValidation
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case http.StatusNotFound:
		return sdkgo.FailureNotFound
	case http.StatusConflict, http.StatusPreconditionFailed:
		return sdkgo.FailureConflict
	default:
		return sdkgo.FailureProviderRejection
	}
}

func graphFailure(kind sdkgo.FailureKind, operationID string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operationID, Message: message}
}

func failurePointer(kind sdkgo.FailureKind, operationID string, message string) *sdkgo.Failure {
	failure := graphFailure(kind, operationID, message)
	return &failure
}

func graphRequestID(header http.Header) string {
	if value := header.Get("request-id"); value != "" {
		return value
	}
	return header.Get("client-request-id")
}

// queryAttemptFromExchange converts a failed exchange into a Query Retry or the operation's branch.
func queryAttemptFromExchange[T any](client *Client, call sdkgo.Call, exchange graphExchange, branches failureBranches) sdkgo.QueryAttempt[T] {
	if exchange.outcome == exchangeRetry {
		return sdkgo.NewQueryRetry[T](exchange.failure, exchange.retryAfter)
	}
	var zero T
	failure, receipt := exchangeFailureAndReceipt(client, call, exchange)
	return sdkgo.NewQueryBranch(branches.branchFor(exchange.outcome), zero, &failure, receipt)
}

// mutationAttemptFromExchange converts a failed exchange into a Mutation Retry or the operation's branch.
func mutationAttemptFromExchange[T any](client *Client, call sdkgo.Call, exchange graphExchange, branches failureBranches) sdkgo.MutationAttempt[T] {
	if exchange.outcome == exchangeRetry {
		return sdkgo.NewMutationRetry[T](exchange.failure, exchange.retryAfter)
	}
	var zero T
	failure, receipt := exchangeFailureAndReceipt(client, call, exchange)
	return sdkgo.NewMutationBranch(branches.branchFor(exchange.outcome), zero, &failure, receipt)
}

// exchangeFailureAndReceipt returns a failed exchange's Failure and, when a provider answered, its Receipt.
func exchangeFailureAndReceipt(client *Client, call sdkgo.Call, exchange graphExchange) (sdkgo.Failure, sdkgo.Receipt) {
	if exchange.outcome == exchangeDefect {
		return exchange.failure, sdkgo.Receipt{}
	}
	return exchange.failure, client.receipt(call, exchange.response, "")
}
