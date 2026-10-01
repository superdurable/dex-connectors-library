// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package docs implements bounded Google Docs operations: reading a document
// as Markdown or plain text with its revision ID, creating a document in a
// Drive folder without a duplicate on retry, and replacing or appending text
// only at the revision a Flow read.
//
// Applications use the generated operation-specific Step factories, such as
// NewGetDocumentTextStep and NewReplaceDocumentTextStep, with a Connection
// built by NewLocalConnection or NewConnection. The runnable example in
// examples/policy-publish uses every operation in one Flow.
package docs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	docsProviderName      = "google-docs"
	docsAPIPath           = "/v1/documents"
	driveAPIPath          = "/drive/v3"
	driveUploadAPIPath    = "/upload/drive/v3"
	defaultRequestTimeout = 25 * time.Second
	// maxTextBytesLimit keeps a createDocument multipart body under Google's 5 MB multipart limit.
	maxTextBytesLimit = 4 << 20
	// googleDocumentMimeType is the Drive MIME type of a Google Doc.
	googleDocumentMimeType = "application/vnd.google-apps.document"
)

// driveIDPattern accepts Google Docs document IDs and Drive folder IDs, which share one alphabet.
var driveIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

// tabIDPattern accepts the Docs tab IDs Google returns, such as t.0.
var tabIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,256}$`)

// googleErrorTokenPointers locate Google's machine-readable error reason and status.
var googleErrorTokenPointers = []string{"/error/errors/0/reason", "/error/status", "/error/details/0/reason"}

// googleRateLimitTokens are error reasons and statuses Google uses for retryable rate limits.
var googleRateLimitTokens = map[string]bool{"rateLimitExceeded": true, "userRateLimitExceeded": true, "RATE_LIMIT_EXCEEDED": true}

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
}

// WithHTTPClient overrides the default 25-second HTTP client used for Docs,
// Drive, and token requests. The connector uses a copy that never follows
// redirects; the caller retains ownership of the original client and its transport.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Google Docs and Drive requests for connector
// operations. A Client is immutable after New and safe for concurrent use by Dex Workers.
type Client struct {
	documentsBaseURL   string
	driveBaseURL       string
	driveUploadBaseURL string
	httpClient         *http.Client
	credentials        sdkgo.CredentialProvider[Credentials]
	refreshDriver      sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes   int64
	maxTextBytes       int64
	now                func() time.Time
}

// googleRequest is one Google HTTP request; body is resent once after a 401 forces a refresh.
type googleRequest struct {
	method        string
	target        string
	body          []byte
	contentType   string
	responseLimit int64
}

type googleResponse struct {
	status    int
	header    http.Header
	body      []byte
	requestID string
}

type googleRequestError struct {
	kind    sdkgo.FailureKind
	message string
}

// Error returns the safe human-readable failure message.
func (failure *googleRequestError) Error() string { return failure.message }

// readBranches names the branches one operation selects for a failed Google read.
type readBranches struct {
	operationID      string
	notFound         sdkgo.BranchID
	tooLarge         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// readOutcome classifies a failed Google read; an empty branch means Retry.
type readOutcome struct {
	branch     sdkgo.BranchID
	failure    sdkgo.Failure
	retryAfter time.Duration
	receipt    sdkgo.Receipt
}

// New validates configuration and constructs an authenticated Google Docs client.
// It fails when an endpoint is not HTTPS (loopback HTTP is accepted for tests),
// a limit is outside its documented range, or credentials is nil.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	docsEndpoint, err := providerhttp.ValidateBaseURL(config.DocsEndpoint)
	if err != nil {
		return nil, fmt.Errorf("Google Docs endpoint: %w", err)
	}
	driveEndpoint, err := providerhttp.ValidateBaseURL(config.DriveEndpoint)
	if err != nil {
		return nil, fmt.Errorf("Google Drive endpoint: %w", err)
	}
	if credentials == nil {
		return nil, errors.New("credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Google Docs connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.httpClient == nil {
		dependencies.httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Google Docs response limit must be positive")
	}
	if config.MaxTextBytes < 1 || config.MaxTextBytes > maxTextBytesLimit {
		return nil, fmt.Errorf("Google Docs text limit must be from 1 to %d bytes", maxTextBytesLimit)
	}
	return &Client{
		documentsBaseURL: docsEndpoint + docsAPIPath,
		driveBaseURL:     driveEndpoint + driveAPIPath, driveUploadBaseURL: driveEndpoint + driveUploadAPIPath,
		httpClient:       providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials:      credentials,
		refreshDriver:    NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes: config.MaxResponseBytes, maxTextBytes: config.MaxTextBytes,
		now: dependencies.now,
	}, nil
}

// GetDocumentText returns the GetDocumentText operation bound to this client.
func (client *Client) GetDocumentText() GetDocumentTextOperation {
	return GetDocumentTextOperation{client: client}
}

// CreateDocument returns the CreateDocument operation bound to this client.
func (client *Client) CreateDocument() CreateDocumentOperation {
	return CreateDocumentOperation{client: client}
}

// ReplaceDocumentText returns the ReplaceDocumentText operation bound to this client.
func (client *Client) ReplaceDocumentText() ReplaceDocumentTextOperation {
	return ReplaceDocumentTextOperation{client: client}
}

// AppendText returns the AppendText operation bound to this client.
func (client *Client) AppendText() AppendTextOperation { return AppendTextOperation{client: client} }

func (client *Client) resolveCredential(call sdkgo.Call, operationID string) (Credentials, *sdkgo.Failure) {
	credential, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credential) != nil {
		return Credentials{}, docsFailurePointer(sdkgo.FailureAuthentication, operationID, "connection credentials are unavailable")
	}
	return credential, nil
}

// sendRead performs one idempotent Google read and classifies every non-2xx or unreadable response.
func (client *Client) sendRead(
	call sdkgo.Call,
	credential *Credentials,
	branches readBranches,
	request googleRequest,
) (googleResponse, *readOutcome) {
	response, err := client.sendRequest(call, credential, request)
	receipt := client.receipt(call, response.requestID, "")
	var requestErr *googleRequestError
	if errors.As(err, &requestErr) {
		switch requestErr.kind {
		case sdkgo.FailureLocalDefect:
			return response, &readOutcome{branch: branches.defect, failure: docsFailure(requestErr.kind, branches.operationID, requestErr.message)}
		case sdkgo.FailureResponseTooLarge:
			return response, &readOutcome{branch: branches.tooLarge, failure: docsFailure(requestErr.kind, branches.operationID, requestErr.message), receipt: receipt}
		}
	}
	if err != nil {
		return response, &readOutcome{failure: docsFailure(sdkgo.FailureTransport, branches.operationID, "provider is unavailable")}
	}
	if response.status >= 200 && response.status < 300 {
		return response, nil
	}
	tokens := providerhttp.ReadErrorTokens(response.body, googleErrorTokenPointers)
	if isRetryableStatus(response.status, tokens) {
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return response, &readOutcome{failure: docsFailure(statusFailureKind(response.status, tokens), branches.operationID, "provider temporarily rejected the request"), retryAfter: delay}
	}
	if response.status == http.StatusNotFound {
		return response, &readOutcome{branch: branches.notFound, failure: docsFailure(sdkgo.FailureNotFound, branches.operationID, "document was not found or is not visible to the connection"), receipt: receipt}
	}
	return response, &readOutcome{branch: branches.providerRejected, failure: docsFailure(statusFailureKind(response.status, tokens), branches.operationID, "provider rejected the request"), receipt: receipt}
}

// sendRequest sends request with the resolved credential. After one 401 it
// forces a single coordinated refresh, updates credential, and sends once more.
func (client *Client) sendRequest(call sdkgo.Call, credential *Credentials, request googleRequest) (googleResponse, error) {
	for attempt := 0; attempt < 2; attempt++ {
		var body io.Reader
		if request.body != nil {
			body = bytes.NewReader(request.body)
		}
		httpRequest, err := http.NewRequestWithContext(call.Context, request.method, request.target, body)
		if err != nil {
			return googleResponse{}, &googleRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be built"}
		}
		httpRequest.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
		if request.contentType != "" {
			httpRequest.Header.Set("Content-Type", request.contentType)
		}
		httpResponse, err := client.httpClient.Do(httpRequest)
		if err != nil {
			return googleResponse{}, &googleRequestError{kind: sdkgo.FailureTransport, message: "provider request failed"}
		}
		// Error bodies get their own bound, so a small success limit never hides an error status.
		content, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, max(request.responseLimit, providerhttp.MaxErrorBodyBytes))
		closeErr := httpResponse.Body.Close()
		response := googleResponse{status: httpResponse.StatusCode, header: httpResponse.Header, body: content, requestID: googleRequestID(httpResponse.Header)}
		isSuccess := response.status >= 200 && response.status < 300
		isOversized := errors.Is(readErr, providerhttp.ErrBodyTooLarge) || (isSuccess && int64(len(content)) > request.responseLimit)
		if isOversized && isSuccess {
			response.body = nil
			return response, &googleRequestError{kind: sdkgo.FailureResponseTooLarge, message: "provider response exceeds the configured limit"}
		}
		if isOversized {
			// An oversized error body carries no usable reason, so the status alone classifies it.
			response.body = nil
			readErr = nil
		}
		if readErr != nil || closeErr != nil {
			return response, &googleRequestError{kind: sdkgo.FailureTransport, message: "provider response could not be read"}
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
	return googleResponse{}, &googleRequestError{kind: sdkgo.FailureLocalDefect, message: "authenticated request retry was exhausted"}
}

func (client *Client) receipt(call sdkgo.Call, requestID string, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: docsProviderName,
		ProviderObjectID: objectID, ProviderRequestID: requestID, ObservedAt: client.now().UTC(),
	}
}

// documentURL reads one document with every tab, so the first tab's ID is known.
func (client *Client) documentURL(documentID string, suggestionsViewMode string, fields string) string {
	query := url.Values{"includeTabsContent": {"true"}, "suggestionsViewMode": {suggestionsViewMode}, "fields": {fields}}
	return client.documentsBaseURL + "/" + url.PathEscape(documentID) + "?" + query.Encode()
}

func (client *Client) batchUpdateURL(documentID string) string {
	return client.documentsBaseURL + "/" + url.PathEscape(documentID) + ":batchUpdate"
}

// filesListURL searches My Drive, shared-with-me files, and every shared drive the account can see.
func (client *Client) filesListURL(query url.Values) string {
	query.Set("corpora", "allDrives")
	query.Set("supportsAllDrives", "true")
	query.Set("includeItemsFromAllDrives", "true")
	return client.driveBaseURL + "/files?" + query.Encode()
}

func (client *Client) metadataCreateURL() string {
	query := url.Values{"fields": {createdFileFields}, "supportsAllDrives": {"true"}}
	return client.driveBaseURL + "/files?" + query.Encode()
}

func (client *Client) multipartCreateURL() string {
	query := url.Values{"uploadType": {"multipart"}, "fields": {createdFileFields}, "supportsAllDrives": {"true"}}
	return client.driveUploadBaseURL + "/files?" + query.Encode()
}

func validateResolvedCredentials(credentials Credentials) error {
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Google Docs access token is missing or invalid")
	}
	switch credentials.AuthMethodID {
	case "", GoogleOAuthAuthMethodID, WorkspaceDomainDelegationAuthMethodID:
		return nil
	default:
		return errors.New("Google Docs authorization method is invalid")
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

// mutationAttemptFromReadOutcome converts a failed pre-write read into a Mutation Retry or branch.
func mutationAttemptFromReadOutcome[T any](outcome *readOutcome) sdkgo.MutationAttempt[T] {
	if outcome.branch == "" {
		return sdkgo.NewMutationRetry[T](outcome.failure, outcome.retryAfter)
	}
	var zero T
	failure := outcome.failure
	return sdkgo.NewMutationBranch(outcome.branch, zero, &failure, outcome.receipt)
}

func isDriveID(value string) bool { return driveIDPattern.MatchString(value) }

func isRetryableStatus(status int, tokens []string) bool {
	return status >= 500 || status == http.StatusRequestTimeout || isRateLimitStatus(status, tokens)
}

// isRateLimitStatus reports a 429 or a 403 whose Google reason or status is a rate limit.
func isRateLimitStatus(status int, tokens []string) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	if status != http.StatusForbidden {
		return false
	}
	for _, token := range tokens {
		if googleRateLimitTokens[token] {
			return true
		}
	}
	return false
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
	case status >= 500 || status == http.StatusRequestTimeout:
		return sdkgo.FailureAvailability
	default:
		return sdkgo.FailureProviderRejection
	}
}

func docsFailure(kind sdkgo.FailureKind, operationID, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: docsProviderName, Operation: operationID, Message: message}
}

func docsFailurePointer(kind sdkgo.FailureKind, operationID, message string) *sdkgo.Failure {
	failure := docsFailure(kind, operationID, message)
	return &failure
}

func googleRequestID(header http.Header) string {
	if value := header.Get("X-Goog-Request-Id"); value != "" {
		return value
	}
	return header.Get("X-Request-Id")
}
