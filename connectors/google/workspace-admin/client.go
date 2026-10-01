// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package workspaceadmin implements Google Workspace user-account and
// group-membership operations through the Admin SDK Directory API as Dex
// connector Steps: read and list accounts, create one without duplicating it,
// suspend and restore it, and add it to or remove it from a group.
//
// Applications use the generated operation-specific Step factories, such as
// NewCreateUserStep and NewAddUserToGroupStep, with a Connection built by
// NewLocalConnection or NewConnection. Every operation acts with the
// privileges of one Workspace administrator: the delegated administrator of
// the domain-wide delegation method, or the administrator who consented to
// the Google OAuth method. The runnable example in examples/account-lifecycle
// onboards, reinstates, and offboards one account.
package workspaceadmin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName          = "google-workspace-admin"
	directoryAPIPath      = "/admin/directory/v1"
	defaultRequestTimeout = 25 * time.Second
	// maxListUsersPageSize is Google's documented users.list maximum.
	maxListUsersPageSize = 500
	// userCreationIncompleteMessage is the error text Google documents for a call that races account creation.
	userCreationIncompleteMessage = "User creation is not complete"
)

// googleErrorTokenPointers locate Google's machine-readable error reason and status.
var googleErrorTokenPointers = []string{"/error/errors/0/reason", "/error/status"}

// retryableForbiddenReasons are the 403 reasons Google documents as retryable Directory API limits.
var retryableForbiddenReasons = map[string]bool{"userRateLimitExceeded": true, "rateLimitExceeded": true, "quotaExceeded": true}

// reportableGoogleErrorReasons are Google reason codes that are safe to repeat in a Failure message.
var reportableGoogleErrorReasons = map[string]bool{
	"badRequest": true, "invalid": true, "required": true, "notFound": true, "duplicate": true,
	"conflict": true, "conditionNotMet": true, "failedPrecondition": true, "forbidden": true,
	"insufficientPermissions": true, "authError": true, "limitExceeded": true, "backendError": true,
	"userRateLimitExceeded": true, "rateLimitExceeded": true, "quotaExceeded": true,
}

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

// WithHTTPClient overrides the default 25-second HTTP client used for Directory
// API and token requests. The connector uses a copy that never follows
// redirects; the caller retains ownership of the original client and its transport.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Admin SDK Directory API requests for connector operations.
// A Client is immutable after New and safe for concurrent use by Dex Workers.
type Client struct {
	apiBaseURL        string
	httpClient        *http.Client
	credentials       sdkgo.CredentialProvider[Credentials]
	refreshDriver     sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes  int64
	listUsersPageSize int
	now               func() time.Time
}

// directoryRequest is one Directory API request; its encoded payload is resent once after a 401 forces a refresh.
type directoryRequest struct {
	method  string
	path    string
	query   url.Values
	payload any
}

type directoryResponse struct {
	status    int
	header    http.Header
	body      []byte
	requestID string
}

// directoryRequestError is a safe local classification of a request that produced no usable response.
type directoryRequestError struct {
	kind    sdkgo.FailureKind
	message string
}

// Error returns the safe human-readable failure message.
func (failure *directoryRequestError) Error() string { return failure.message }

// exchangeOutcome is the provider-neutral meaning of one Directory API exchange.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeRetry
	exchangeNotFound
	exchangeConflict
	exchangeRejected
	exchangeInvalidResponse
	exchangeDefect
)

// directoryExchange is one classified exchange that each operation maps to its own branch or Retry.
type directoryExchange struct {
	outcome    exchangeOutcome
	response   directoryResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// failureBranches names the branch one operation selects for each non-success exchange that is not retried.
type failureBranches struct {
	notFound        sdkgo.BranchID
	conflict        sdkgo.BranchID
	rejected        sdkgo.BranchID
	invalidResponse sdkgo.BranchID
	defect          sdkgo.BranchID
}

func (branches failureBranches) branchFor(outcome exchangeOutcome) sdkgo.BranchID {
	switch outcome {
	case exchangeNotFound:
		return branches.notFound
	case exchangeConflict:
		return branches.conflict
	case exchangeRejected:
		return branches.rejected
	case exchangeInvalidResponse:
		return branches.invalidResponse
	default:
		return branches.defect
	}
}

// userRead is one classified user exchange with its decoded account.
type userRead struct {
	exchange directoryExchange
	user     User
	resource directoryUser
}

type googleErrorEnvelope struct {
	Error struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
			Reason  string `json:"reason"`
		} `json:"errors"`
	} `json:"error"`
}

// New validates configuration and constructs an authenticated Directory API client.
// It fails when the endpoint is not HTTPS (loopback HTTP is accepted for tests),
// a limit is outside its documented range, or credentials is nil.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Google Workspace Admin endpoint: %w", err)
	}
	if credentials == nil {
		return nil, errors.New("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Google Workspace Admin connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.httpClient == nil {
		dependencies.httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Google Workspace Admin response limit must be positive")
	}
	if config.ListUsersPageSize < 1 || config.ListUsersPageSize > maxListUsersPageSize {
		return nil, fmt.Errorf("Google Workspace Admin listUsers page size must be from 1 to %d", maxListUsersPageSize)
	}
	return &Client{
		apiBaseURL:        endpoint + directoryAPIPath,
		httpClient:        providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials:       credentials,
		refreshDriver:     NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes:  config.MaxResponseBytes,
		listUsersPageSize: int(config.ListUsersPageSize),
		now:               time.Now,
	}, nil
}

// GetUser returns the getUser Query bound to this client.
func (client *Client) GetUser() GetUserOperation { return GetUserOperation{client: client} }

// ListUsers returns the listUsers Query bound to this client.
func (client *Client) ListUsers() ListUsersOperation { return ListUsersOperation{client: client} }

// CreateUser returns the createUser Mutation bound to this client.
func (client *Client) CreateUser() CreateUserOperation { return CreateUserOperation{client: client} }

// SuspendUser returns the suspendUser Mutation bound to this client.
func (client *Client) SuspendUser() SuspendUserOperation { return SuspendUserOperation{client: client} }

// UnsuspendUser returns the unsuspendUser Mutation bound to this client.
func (client *Client) UnsuspendUser() UnsuspendUserOperation {
	return UnsuspendUserOperation{client: client}
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

// readUser performs one users.get request; an undecodable account becomes an invalid-response exchange.
func (client *Client) readUser(call sdkgo.Call, credential *Credentials, operationID string, userKey string) userRead {
	return client.decodeUserExchange(operationID, client.exchange(call, credential, operationID, directoryRequest{method: http.MethodGet, path: userPath(userKey)}))
}

// writeUserSuspension sets one account's suspended flag with users.update, which Google applies with patch semantics.
func (client *Client) writeUserSuspension(call sdkgo.Call, credential *Credentials, operationID string, userKey string, isSuspended bool) userRead {
	return client.decodeUserExchange(operationID, client.exchange(call, credential, operationID, directoryRequest{
		method: http.MethodPut, path: userPath(userKey), payload: map[string]bool{"suspended": isSuspended},
	}))
}

// decodeUserExchange decodes a successful account response; an undecodable account becomes an invalid-response exchange.
func (client *Client) decodeUserExchange(operationID string, exchange directoryExchange) userRead {
	if exchange.outcome != exchangeSucceeded {
		return userRead{exchange: exchange}
	}
	user, resource, err := decodeUser(exchange.response.body)
	if err != nil {
		return userRead{exchange: client.invalidResponseExchange(operationID, exchange.response, "provider returned an invalid account: "+err.Error())}
	}
	return userRead{exchange: exchange, user: user, resource: resource}
}

// invalidResponseExchange reclassifies a successful exchange whose body is unusable.
func (client *Client) invalidResponseExchange(operationID string, response directoryResponse, message string) directoryExchange {
	return directoryExchange{outcome: exchangeInvalidResponse, response: response, failure: directoryFailure(sdkgo.FailureProtocol, operationID, message)}
}

// exchange sends one request and classifies the response without choosing an operation branch.
func (client *Client) exchange(call sdkgo.Call, credential *Credentials, operationID string, request directoryRequest) directoryExchange {
	response, err := client.send(call, credential, request)
	var requestErr *directoryRequestError
	if errors.As(err, &requestErr) {
		switch requestErr.kind {
		case sdkgo.FailureLocalDefect:
			return directoryExchange{outcome: exchangeDefect, failure: directoryFailure(requestErr.kind, operationID, requestErr.message)}
		case sdkgo.FailureResponseTooLarge:
			return directoryExchange{outcome: exchangeInvalidResponse, response: response, failure: directoryFailure(requestErr.kind, operationID, requestErr.message)}
		}
	}
	if err != nil {
		return directoryExchange{outcome: exchangeRetry, response: response, failure: directoryFailure(sdkgo.FailureTransport, operationID, "provider is unavailable")}
	}
	if isSuccessStatus(response.status) {
		return directoryExchange{outcome: exchangeSucceeded, response: response}
	}
	return client.classifyFailureStatus(operationID, response)
}

// classifyFailureStatus maps a non-2xx response; Google reports some rate limits as 403 with a reason.
func (client *Client) classifyFailureStatus(operationID string, response directoryResponse) directoryExchange {
	tokens := providerhttp.ReadErrorTokens(response.body, googleErrorTokenPointers)
	reason := ""
	if len(tokens) > 0 {
		reason = tokens[0]
	}
	switch {
	case hasUserCreationIncompleteMessage(response.body):
		return client.retryExchange(operationID, response, sdkgo.FailureAvailability, "Google is still creating the account")
	case response.status == http.StatusTooManyRequests,
		response.status == http.StatusForbidden && retryableForbiddenReasons[reason]:
		return client.retryExchange(operationID, response, sdkgo.FailureRateLimit, describeReason("provider rate limited the request", reason))
	case response.status == http.StatusRequestTimeout || response.status >= 500:
		return client.retryExchange(operationID, response, sdkgo.FailureAvailability, describeReason("provider is temporarily unavailable", reason))
	case response.status == http.StatusNotFound:
		return directoryExchange{outcome: exchangeNotFound, response: response, failure: directoryFailure(sdkgo.FailureNotFound, operationID, "resource was not found")}
	case response.status == http.StatusConflict:
		return directoryExchange{outcome: exchangeConflict, response: response, failure: directoryFailure(sdkgo.FailureConflict, operationID, describeReason("provider reported a conflicting resource", reason))}
	default:
		message := describeReason(fmt.Sprintf("provider rejected the request with HTTP %d", response.status), reason)
		return directoryExchange{outcome: exchangeRejected, response: response, failure: directoryFailure(statusFailureKind(response.status), operationID, message)}
	}
}

// retryExchange asks Dex to retry after Google's Retry-After delay, capped at one hour.
func (client *Client) retryExchange(operationID string, response directoryResponse, kind sdkgo.FailureKind, message string) directoryExchange {
	return directoryExchange{
		outcome: exchangeRetry, response: response, failure: directoryFailure(kind, operationID, message),
		retryAfter: providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now()),
	}
}

// send issues one authenticated request. After a 401 it forces one coordinated refresh, updates credential, and sends once more.
func (client *Client) send(call sdkgo.Call, credential *Credentials, request directoryRequest) (directoryResponse, error) {
	var encodedPayload []byte
	if request.payload != nil {
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return directoryResponse{}, &directoryRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be encoded"}
		}
		encodedPayload = encoded
	}
	target := client.apiBaseURL + request.path
	if len(request.query) > 0 {
		target += "?" + request.query.Encode()
	}
	for attempt := 0; attempt < 2; attempt++ {
		var body io.Reader
		if encodedPayload != nil {
			body = bytes.NewReader(encodedPayload)
		}
		httpRequest, err := http.NewRequestWithContext(call.Context, request.method, target, body)
		if err != nil {
			return directoryResponse{}, &directoryRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be built"}
		}
		httpRequest.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
		httpRequest.Header.Set("Accept", "application/json")
		if encodedPayload != nil {
			httpRequest.Header.Set("Content-Type", "application/json")
		}
		httpResponse, err := client.httpClient.Do(httpRequest)
		if err != nil {
			return directoryResponse{}, &directoryRequestError{kind: sdkgo.FailureTransport, message: "provider request failed"}
		}
		// Error bodies get their own bound, so a small success limit never hides an error status.
		content, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, max(client.maxResponseBytes, providerhttp.MaxErrorBodyBytes))
		closeErr := httpResponse.Body.Close()
		response := directoryResponse{status: httpResponse.StatusCode, header: httpResponse.Header, body: content, requestID: googleRequestID(httpResponse.Header)}
		isSuccess := isSuccessStatus(response.status)
		isOversized := errors.Is(readErr, providerhttp.ErrBodyTooLarge) || (isSuccess && int64(len(content)) > client.maxResponseBytes)
		if isOversized && isSuccess {
			response.body = nil
			return response, &directoryRequestError{kind: sdkgo.FailureResponseTooLarge, message: "provider response exceeds the configured limit"}
		}
		if isOversized {
			// An oversized error body carries no usable reason, so the status alone classifies it.
			response.body = nil
			readErr = nil
		}
		if readErr != nil || closeErr != nil {
			return response, &directoryRequestError{kind: sdkgo.FailureTransport, message: "provider response could not be read"}
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
	return directoryResponse{}, &directoryRequestError{kind: sdkgo.FailureLocalDefect, message: "authenticated request retry was exhausted"}
}

func (client *Client) receipt(call sdkgo.Call, response directoryResponse, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ProviderRequestID: response.requestID, ObservedAt: client.now().UTC(),
	}
}

func validateResolvedCredentials(credentials Credentials) error {
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Google Workspace Admin access token is missing or invalid")
	}
	switch credentials.AuthMethodID {
	case "", WorkspaceDomainDelegationAuthMethodID, GoogleOAuthAuthMethodID:
		return nil
	default:
		return errors.New("Google Workspace Admin authorization method is invalid")
	}
}

// hasUserCreationIncompleteMessage reports Google's documented propagation error; the text is never repeated.
func hasUserCreationIncompleteMessage(body []byte) bool {
	var envelope googleErrorEnvelope
	if len(body) == 0 || json.Unmarshal(body, &envelope) != nil {
		return false
	}
	if strings.Contains(envelope.Error.Message, userCreationIncompleteMessage) {
		return true
	}
	for _, detail := range envelope.Error.Errors {
		if strings.Contains(detail.Message, userCreationIncompleteMessage) {
			return true
		}
	}
	return false
}

func describeReason(message string, reason string) string {
	if !reportableGoogleErrorReasons[reason] {
		return message
	}
	return message + " (" + reason + ")"
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

func directoryFailure(kind sdkgo.FailureKind, operationID string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operationID, Message: message}
}

func failurePointer(kind sdkgo.FailureKind, operationID string, message string) *sdkgo.Failure {
	failure := directoryFailure(kind, operationID, message)
	return &failure
}

func googleRequestID(header http.Header) string {
	if value := header.Get("X-Goog-Request-Id"); value != "" {
		return value
	}
	return header.Get("X-Request-Id")
}

// queryAttemptFromExchange converts a failed exchange into a Query Retry or the operation's branch.
func queryAttemptFromExchange[T any](client *Client, call sdkgo.Call, exchange directoryExchange, branches failureBranches) sdkgo.QueryAttempt[T] {
	if exchange.outcome == exchangeRetry {
		return sdkgo.NewQueryRetry[T](exchange.failure, exchange.retryAfter)
	}
	var zero T
	failure, receipt := exchangeFailureAndReceipt(client, call, exchange)
	return sdkgo.NewQueryBranch(branches.branchFor(exchange.outcome), zero, &failure, receipt)
}

// mutationAttemptFromExchange converts a failed exchange into a Mutation Retry or the operation's branch.
func mutationAttemptFromExchange[T any](client *Client, call sdkgo.Call, exchange directoryExchange, branches failureBranches) sdkgo.MutationAttempt[T] {
	if exchange.outcome == exchangeRetry {
		return sdkgo.NewMutationRetry[T](exchange.failure, exchange.retryAfter)
	}
	var zero T
	failure, receipt := exchangeFailureAndReceipt(client, call, exchange)
	return sdkgo.NewMutationBranch(branches.branchFor(exchange.outcome), zero, &failure, receipt)
}

// exchangeFailureAndReceipt returns a failed exchange's Failure and, when a provider answered, its Receipt.
func exchangeFailureAndReceipt(client *Client, call sdkgo.Call, exchange directoryExchange) (sdkgo.Failure, sdkgo.Receipt) {
	if exchange.outcome == exchangeDefect {
		return exchange.failure, sdkgo.Receipt{}
	}
	return exchange.failure, client.receipt(call, exchange.response, "")
}
