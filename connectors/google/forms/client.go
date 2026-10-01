// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package forms implements bounded, read-only Google Forms operations: read a
// form's questions, list its responses one page at a time, and read one
// response by ID.
//
// Applications use the generated operation-specific Step factories,
// NewGetFormStep, NewListResponsesStep, and NewGetResponseStep, with a
// Connection built by NewLocalConnection or NewConnection. The runnable
// example in examples/response-recorder shows every operation in one Flow.
package forms

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	formsProviderName     = "google-forms"
	formsAPIPath          = "/v1"
	defaultRequestTimeout = 25 * time.Second
	// maxResponsePageSize bounds every listResponses page, so one Result stays small.
	maxResponsePageSize = 1000
	maxPageTokenBytes   = 4096
)

// googleIDPattern accepts Google Forms form IDs, which are Drive file IDs, and response IDs.
var googleIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

// googleErrorTokenPointers locate Google's canonical status and machine-readable reasons.
var googleErrorTokenPointers = []string{"/error/status", "/error/details/0/reason", "/error/errors/0/reason"}

// googleRateLimitReasons are 403 reasons that Google documents as retryable rate limits.
var googleRateLimitReasons = []string{"RATE_LIMIT_EXCEEDED", "rateLimitExceeded", "userRateLimitExceeded"}

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
}

// WithHTTPClient overrides the default 25-second HTTP client used for Google
// Forms and token requests. The connector uses a copy that never follows
// redirects; the caller retains ownership of the original client and its transport.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Google Forms requests for connector operations.
// A Client is immutable after New and safe for concurrent use by Dex Workers.
type Client struct {
	apiBaseURL       string
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	refreshDriver    sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes int64
	responsePageSize int
	now              func() time.Time
}

// readBranches names the branches one operation selects for a failed Google Forms read.
type readBranches struct {
	operationID      string
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// readOutcome classifies a failed read; an empty branch means Retry.
type readOutcome struct {
	branch     sdkgo.BranchID
	failure    sdkgo.Failure
	retryAfter time.Duration
	receipt    sdkgo.Receipt
}

type formsResponse struct {
	status    int
	header    http.Header
	body      []byte
	requestID string
}

type formsRequestError struct {
	kind    sdkgo.FailureKind
	message string
}

// Error returns the safe human-readable failure message.
func (failure *formsRequestError) Error() string { return failure.message }

// New validates configuration and constructs an authenticated Google Forms
// client. It fails when the endpoint is not HTTPS (loopback HTTP is accepted
// for tests), a limit is outside its documented range, or credentials is nil.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := providerhttp.ValidateBaseURL(config.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("Google Forms endpoint: %w", err)
	}
	if credentials == nil {
		return nil, errors.New("credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Google Forms connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.httpClient == nil {
		dependencies.httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Google Forms response limit must be positive")
	}
	if config.ResponsePageSize < 1 || config.ResponsePageSize > maxResponsePageSize {
		return nil, fmt.Errorf("Google Forms response page size must be from 1 to %d", maxResponsePageSize)
	}
	return &Client{
		apiBaseURL:       endpoint + formsAPIPath,
		httpClient:       providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials:      credentials,
		refreshDriver:    NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes: config.MaxResponseBytes,
		responsePageSize: int(config.ResponsePageSize),
		now:              dependencies.now,
	}, nil
}

// GetForm returns the GetForm operation bound to this client.
func (client *Client) GetForm() GetFormOperation { return GetFormOperation{client: client} }

// ListResponses returns the ListResponses operation bound to this client.
func (client *Client) ListResponses() ListResponsesOperation {
	return ListResponsesOperation{client: client}
}

// GetResponse returns the GetResponse operation bound to this client.
func (client *Client) GetResponse() GetResponseOperation {
	return GetResponseOperation{client: client}
}

func (client *Client) resolveCredential(call sdkgo.Call, operationID string) (Credentials, *sdkgo.Failure) {
	credential, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credential) != nil {
		return Credentials{}, formsFailurePointer(sdkgo.FailureAuthentication, operationID, "connection credentials are unavailable")
	}
	return credential, nil
}

// sendRead performs one idempotent Google Forms GET and classifies every non-2xx or unreadable response.
func (client *Client) sendRead(call sdkgo.Call, credential *Credentials, branches readBranches, target string) (formsResponse, *readOutcome) {
	response, err := client.sendRequest(call, credential, target)
	receipt := client.receipt(call, response.requestID, "")
	var requestErr *formsRequestError
	if errors.As(err, &requestErr) {
		switch requestErr.kind {
		case sdkgo.FailureLocalDefect:
			return response, &readOutcome{branch: branches.defect, failure: formsFailure(requestErr.kind, branches.operationID, requestErr.message)}
		case sdkgo.FailureResponseTooLarge:
			return response, &readOutcome{branch: branches.invalidResponse, failure: formsFailure(requestErr.kind, branches.operationID, requestErr.message), receipt: receipt}
		}
	}
	if err != nil {
		return response, &readOutcome{failure: formsFailure(sdkgo.FailureTransport, branches.operationID, "provider is unavailable")}
	}
	if response.status >= 200 && response.status < 300 {
		return response, nil
	}
	tokens := providerhttp.ReadErrorTokens(response.body, googleErrorTokenPointers)
	if isRetryableStatus(response.status, tokens) {
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return response, &readOutcome{failure: formsFailure(statusFailureKind(response.status, tokens), branches.operationID, "provider temporarily rejected the request"), retryAfter: delay}
	}
	if response.status == http.StatusNotFound {
		return response, &readOutcome{branch: branches.notFound, failure: formsFailure(sdkgo.FailureNotFound, branches.operationID, "form or response was not found or is not visible to the connection"), receipt: receipt}
	}
	return response, &readOutcome{branch: branches.providerRejected, failure: formsFailure(statusFailureKind(response.status, tokens), branches.operationID, "provider rejected the request"), receipt: receipt}
}

// sendRequest sends one GET with the resolved credential. After one 401 it
// forces a single coordinated refresh, updates credential, and sends once more.
func (client *Client) sendRequest(call sdkgo.Call, credential *Credentials, target string) (formsResponse, error) {
	for attempt := 0; attempt < 2; attempt++ {
		httpRequest, err := http.NewRequestWithContext(call.Context, http.MethodGet, target, nil)
		if err != nil {
			return formsResponse{}, &formsRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be built"}
		}
		httpRequest.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
		httpRequest.Header.Set("Accept", "application/json")
		httpResponse, err := client.httpClient.Do(httpRequest)
		if err != nil {
			return formsResponse{}, &formsRequestError{kind: sdkgo.FailureTransport, message: "provider request failed"}
		}
		// Error bodies get their own bound, so a small success limit never hides an error status.
		content, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, max(client.maxResponseBytes, providerhttp.MaxErrorBodyBytes))
		closeErr := httpResponse.Body.Close()
		response := formsResponse{status: httpResponse.StatusCode, header: httpResponse.Header, body: content, requestID: googleRequestID(httpResponse.Header)}
		isSuccess := response.status >= 200 && response.status < 300
		isOversized := errors.Is(readErr, providerhttp.ErrBodyTooLarge) || (isSuccess && int64(len(content)) > client.maxResponseBytes)
		if isOversized && isSuccess {
			response.body = nil
			return response, &formsRequestError{kind: sdkgo.FailureResponseTooLarge, message: "provider response exceeds the configured limit"}
		}
		if isOversized {
			// An oversized error body carries no usable reason, so the status alone classifies it.
			response.body = nil
			readErr = nil
		}
		if readErr != nil || closeErr != nil {
			return response, &formsRequestError{kind: sdkgo.FailureTransport, message: "provider response could not be read"}
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
	return formsResponse{}, &formsRequestError{kind: sdkgo.FailureLocalDefect, message: "authenticated request retry was exhausted"}
}

func (client *Client) receipt(call sdkgo.Call, requestID string, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: formsProviderName,
		ProviderObjectID: objectID, ProviderRequestID: requestID, ObservedAt: client.now().UTC(),
	}
}

func (client *Client) formURL(formID string) string {
	return client.apiBaseURL + "/forms/" + url.PathEscape(formID)
}

func (client *Client) responsesURL(formID string, query url.Values) string {
	return client.formURL(formID) + "/responses?" + query.Encode()
}

func (client *Client) responseURL(formID string, responseID string) string {
	return client.formURL(formID) + "/responses/" + url.PathEscape(responseID)
}

func validateResolvedCredentials(credentials Credentials) error {
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Google Forms access token is missing or invalid")
	}
	switch credentials.AuthMethodID {
	case "", GoogleOAuthAuthMethodID, WorkspaceDomainDelegationAuthMethodID:
		return nil
	default:
		return errors.New("Google Forms authorization method is invalid")
	}
}

// queryAttemptFromReadOutcome converts a failed read into a Query Retry or branch.
func queryAttemptFromReadOutcome[T any](outcome *readOutcome) sdkgo.QueryAttempt[T] {
	if outcome.branch == "" {
		return sdkgo.NewQueryRetry[T](outcome.failure, outcome.retryAfter)
	}
	var zero T
	failure := outcome.failure
	return sdkgo.NewQueryBranch(outcome.branch, zero, &failure, outcome.receipt)
}

func isGoogleID(value string) bool { return googleIDPattern.MatchString(value) }

func isRetryableStatus(status int, tokens []string) bool {
	return status == http.StatusRequestTimeout || status >= 500 || isRateLimitStatus(status, tokens)
}

// isRateLimitStatus reports a 429 or a 403 whose Google reason is a rate limit.
func isRateLimitStatus(status int, tokens []string) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	if status != http.StatusForbidden {
		return false
	}
	return slices.ContainsFunc(tokens, func(token string) bool { return slices.Contains(googleRateLimitReasons, token) })
}

func statusFailureKind(status int, tokens []string) sdkgo.FailureKind {
	switch {
	case status == http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case isRateLimitStatus(status, tokens):
		return sdkgo.FailureRateLimit
	case status == http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case status == http.StatusNotFound:
		return sdkgo.FailureNotFound
	case status == http.StatusConflict:
		return sdkgo.FailureConflict
	case status == http.StatusRequestTimeout || status >= 500:
		return sdkgo.FailureAvailability
	default:
		return sdkgo.FailureProviderRejection
	}
}

func formsFailure(kind sdkgo.FailureKind, operationID, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: formsProviderName, Operation: operationID, Message: message}
}

func formsFailurePointer(kind sdkgo.FailureKind, operationID, message string) *sdkgo.Failure {
	failure := formsFailure(kind, operationID, message)
	return &failure
}

func googleRequestID(header http.Header) string {
	if value := header.Get("X-Goog-Request-Id"); value != "" {
		return value
	}
	return header.Get("X-Request-Id")
}
