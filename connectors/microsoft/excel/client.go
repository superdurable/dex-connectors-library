// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package excel connects Dex Flows to Excel workbooks stored in OneDrive for
// work or school and SharePoint document libraries through the Microsoft
// Graph workbook API at https://graph.microsoft.com/v1.0.
//
// A Client exposes four operations. GetTableRows reads an Excel table as rows
// keyed by its column names, GetValues reads a bounded A1 range, UpdateValues
// writes literal values to a bounded A1 range, and AppendTableRows appends
// table rows whose key column value the table does not hold yet. Every
// operation addresses its workbook by drive ID and drive item ID, which the
// workbookPicker Studio unit derives.
//
// Requests are sessionless: each one carries no workbook-session-id header, so
// Excel persists a write as soon as it answers. Every operation makes at most
// three requests, sessions would add a create and a close request, a session
// expires after about five minutes without use, and Dex retries can run on
// another Worker, so a session would not survive between attempts anyway.
//
// Microsoft Graph has no idempotency key for workbook writes. UpdateValues
// writes absolute values, so a repeated dispatch writes the same values again.
// AppendTableRows reads the key column before it writes, runs with sync
// durability, and records a Dex heartbeat checkpoint before it sends the
// append, so one Step execution sends it at most once.
//
// The runnable example in examples/approval-decision uses every operation in
// one Flow.
package excel

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// OAuthAuthMethodID identifies delegated Microsoft OAuth authorization with a work or school account.
	OAuthAuthMethodID = "microsoft-oauth"
	// MaximumCellsLimit is the largest maxCells configuration value the connector accepts.
	MaximumCellsLimit = 100000

	providerName = "microsoft-excel"
	graphHost    = "graph.microsoft.com"
	loginHost    = "login.microsoftonline.com"
	graphBaseURL = "https://" + graphHost + "/v1.0"

	// defaultRequestTimeout bounds one Graph request inside the 30-second Execute timeout.
	defaultRequestTimeout = 20 * time.Second
	requestIDHeader       = "request-id"
)

var (
	// graphErrorTokenPointers read Excel's second-level code before Graph's top-level code, as Microsoft's error handling guide orders them.
	graphErrorTokenPointers = []string{"/error/innerError/code", "/error/innererror/code", "/error/code"}
	requestIDPattern        = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
)

// graphErrorClass is what a non-2xx Graph response means for the request that received it.
type graphErrorClass uint8

const (
	// graphErrorRetryable means the request may succeed after a cooldown, such as a throttled or transient failure.
	graphErrorRetryable graphErrorClass = iota + 1
	// graphErrorNotFound means the workbook, worksheet, table, or column does not exist or is not visible.
	graphErrorNotFound
	// graphErrorTooLarge means the request or the range exceeds an Excel limit.
	graphErrorTooLarge
	// graphErrorRejected means Microsoft does not expect the same request to succeed again.
	graphErrorRejected
)

// excelErrorClasses maps lowercased Excel and Graph error codes to Microsoft's documented handling.
var excelErrorClasses = map[string]graphErrorClass{
	"accessconflict":                   graphErrorRetryable,
	"gatewaytimeoutuncategorized":      graphErrorRetryable,
	"serviceunavailableuncategorized":  graphErrorRetryable,
	"toomanyrequestsuncategorized":     graphErrorRetryable,
	"transientfailure":                 graphErrorRetryable,
	"activitylimitreached":             graphErrorRetryable,
	"notfounduncategorized":            graphErrorNotFound,
	"itemnotfound":                     graphErrorNotFound,
	"payloadtoolargeuncategorized":     graphErrorTooLarge,
	"rangeexceedslimit":                graphErrorTooLarge,
	"accessdenied":                     graphErrorRejected,
	"badrequestuncategorized":          graphErrorRejected,
	"conflictuncategorized":            graphErrorRejected,
	"filteredrangeconflict":            graphErrorRejected,
	"forbiddenuncategorized":           graphErrorRejected,
	"generalexception":                 graphErrorRejected,
	"insertdeleteconflict":             graphErrorRejected,
	"internalservererroruncategorized": graphErrorRejected,
	"invalidargument":                  graphErrorRejected,
	"invalidreference":                 graphErrorRejected,
	"itemalreadyexists":                graphErrorRejected,
	"methodnotalloweduncategorized":    graphErrorRejected,
	"nonblankcelloffsheet":             graphErrorRejected,
	"notimplementeduncategorized":      graphErrorRejected,
	"requestaborted":                   graphErrorRejected,
	"unauthorizeduncategorized":        graphErrorRejected,
	"unsupportedoperation":             graphErrorRejected,
	"unsupportedworkbook":              graphErrorRejected,
}

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient       *http.Client
	localProviderURL string
	now              func() time.Time
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 20 seconds per request.
// The caller retains ownership of the client and its transport. The connector uses a copy
// that never follows redirects, so a token is never replayed to another host. The same
// client carries refresh requests to https://login.microsoftonline.com.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithLocalProviderURL sends every request for https://graph.microsoft.com and
// https://login.microsoftonline.com to one local Graph-compatible fake, keeping each
// request's path and query, for local verification only. The URL must be an http or https
// URL whose host is loopback, without a path, user information, a query, or a fragment,
// and the transport refuses every other host. Production connections leave it unset.
func WithLocalProviderURL(baseURL string) Option {
	return func(options *clientOptions) { options.localProviderURL = baseURL }
}

// WithClock overrides the clock used for Receipts, Retry-After dates, and token expiry.
// Tests use it; production connections leave it unset.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// Client executes authenticated Microsoft Graph workbook requests for connector
// operations. A Client is immutable after New and safe for concurrent use by Dex Workers.
type Client struct {
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	refreshDriver    *CredentialRefreshDriver
	maxResponseBytes int64
	maxCells         int64
	now              func() time.Time
}

// graphRequest is one Graph request; body is resent once after a 401 forces a refresh.
type graphRequest struct {
	method        string
	target        string
	body          []byte
	responseLimit int64
}

// graphResponse holds the safe parts of one response; an oversized 2xx body is dropped.
type graphResponse struct {
	status         int
	header         http.Header
	body           []byte
	requestID      string
	isBodyTooLarge bool
}

// graphTransportError reports that no complete Graph response was received.
type graphTransportError struct {
	isRequestBuilt bool
}

// Error returns a safe message that never repeats a URL, header, or body.
func (failure *graphTransportError) Error() string {
	if !failure.isRequestBuilt {
		return "Microsoft Graph request could not be built"
	}
	return "Microsoft Graph request failed or its response could not be read"
}

// readBranches names the branches one operation selects for a failed Graph read.
type readBranches struct {
	operationID      string
	notFound         sdkgo.BranchID
	tooLarge         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// readOutcome classifies a failed Graph read; an empty branch means Retry.
type readOutcome struct {
	branch     sdkgo.BranchID
	failure    sdkgo.Failure
	retryAfter time.Duration
	receipt    sdkgo.Receipt
}

// New validates configuration and constructs an authenticated Excel client.
// It fails when a limit is outside its documented range, the local provider URL
// is not a loopback URL, or credentials is nil.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, errors.New("credential provider is required")
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Microsoft Excel response limit must be positive")
	}
	if config.MaxCells < 1 || config.MaxCells > MaximumCellsLimit {
		return nil, fmt.Errorf("Microsoft Excel cell limit must be from 1 to %d", MaximumCellsLimit)
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Microsoft Excel connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("Microsoft Excel clock is required")
	}
	httpClient := dependencies.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	if dependencies.localProviderURL != "" {
		routed, err := newLocalProviderHTTPClient(httpClient, dependencies.localProviderURL)
		if err != nil {
			return nil, err
		}
		httpClient = routed
	}
	refreshDriver := NewCredentialRefreshDriver(httpClient)
	refreshDriver.now = dependencies.now
	return &Client{
		httpClient:       providerhttp.NewProviderHTTPClient(httpClient, defaultRequestTimeout),
		credentials:      credentials,
		refreshDriver:    refreshDriver,
		maxResponseBytes: config.MaxResponseBytes,
		maxCells:         config.MaxCells,
		now:              dependencies.now,
	}, nil
}

// GetTableRows returns the GetTableRows operation bound to this client.
func (client *Client) GetTableRows() GetTableRowsOperation {
	return GetTableRowsOperation{client: client}
}

// GetValues returns the GetValues operation bound to this client.
func (client *Client) GetValues() GetValuesOperation { return GetValuesOperation{client: client} }

// UpdateValues returns the UpdateValues operation bound to this client.
func (client *Client) UpdateValues() UpdateValuesOperation {
	return UpdateValuesOperation{client: client}
}

// AppendTableRows returns the AppendTableRows operation bound to this client.
func (client *Client) AppendTableRows() AppendTableRowsOperation {
	return AppendTableRowsOperation{client: client}
}

func (client *Client) resolveCredential(call sdkgo.Call, operationID string) (Credentials, *sdkgo.Failure) {
	credential, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credential) != nil {
		return Credentials{}, excelFailurePointer(sdkgo.FailureAuthentication, operationID, "connection credentials are unavailable")
	}
	return credential, nil
}

// sendRead performs one idempotent Graph read and classifies every non-2xx or unreadable response.
func (client *Client) sendRead(
	call sdkgo.Call,
	credential *Credentials,
	branches readBranches,
	request graphRequest,
) (graphResponse, *readOutcome) {
	response, err := client.sendRequest(call, credential, request)
	receipt := client.receipt(call, response.requestID)
	var transportErr *graphTransportError
	if errors.As(err, &transportErr) && !transportErr.isRequestBuilt {
		return response, &readOutcome{branch: branches.defect, failure: excelFailure(sdkgo.FailureLocalDefect, branches.operationID, err.Error())}
	}
	if err != nil {
		return response, &readOutcome{failure: excelFailure(sdkgo.FailureTransport, branches.operationID, "provider is unavailable")}
	}
	if isSuccessStatus(response.status) {
		if response.isBodyTooLarge {
			return response, &readOutcome{branch: branches.tooLarge, failure: excelFailure(sdkgo.FailureResponseTooLarge, branches.operationID, "provider response exceeds the configured response limit"), receipt: receipt}
		}
		return response, nil
	}
	tokens := providerhttp.ReadErrorTokens(response.body, graphErrorTokenPointers)
	kind := statusFailureKind(response.status, tokens)
	switch classifyGraphError(response.status, tokens) {
	case graphErrorRetryable:
		delay := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
		return response, &readOutcome{failure: excelFailure(kind, branches.operationID, "provider temporarily rejected the request"), retryAfter: delay}
	case graphErrorNotFound:
		return response, &readOutcome{branch: branches.notFound, failure: excelFailure(sdkgo.FailureNotFound, branches.operationID, "workbook resource was not found or is not visible to the connection"), receipt: receipt}
	case graphErrorTooLarge:
		return response, &readOutcome{branch: branches.tooLarge, failure: excelFailure(sdkgo.FailureResponseTooLarge, branches.operationID, "Excel reported that the range exceeds its limit"), receipt: receipt}
	default:
		return response, &readOutcome{branch: branches.providerRejected, failure: excelFailure(kind, branches.operationID, "provider rejected the request"), receipt: receipt}
	}
}

// sendRequest resends once after a 401 forces one coordinated refresh.
func (client *Client) sendRequest(call sdkgo.Call, credential *Credentials, request graphRequest) (graphResponse, error) {
	for attempt := 0; attempt < 2; attempt++ {
		response, err := client.sendOnce(call, credential, request)
		if err != nil || response.status != http.StatusUnauthorized || attempt != 0 {
			return response, err
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
	return graphResponse{}, &graphTransportError{}
}

func (client *Client) sendOnce(call sdkgo.Call, credential *Credentials, request graphRequest) (graphResponse, error) {
	var body io.Reader
	if request.body != nil {
		body = bytes.NewReader(request.body)
	}
	httpRequest, err := http.NewRequestWithContext(call.Context, request.method, request.target, body)
	if err != nil {
		return graphResponse{}, &graphTransportError{}
	}
	httpRequest.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
	httpRequest.Header.Set("Accept", "application/json")
	if request.body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return graphResponse{}, &graphTransportError{isRequestBuilt: true}
	}
	// Error bodies get their own bound, so a small success limit never hides an error code.
	content, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, max(request.responseLimit, providerhttp.MaxErrorBodyBytes))
	closeErr := httpResponse.Body.Close()
	response := graphResponse{
		status: httpResponse.StatusCode, header: httpResponse.Header, body: content,
		requestID: graphRequestID(httpResponse.Header),
	}
	isOversized := errors.Is(readErr, providerhttp.ErrBodyTooLarge) || int64(len(content)) > request.responseLimit
	if isOversized {
		// An oversized body carries no usable content, so the status alone classifies it.
		response.body = nil
		response.isBodyTooLarge = isSuccessStatus(response.status)
		readErr = nil
	}
	if readErr != nil || closeErr != nil {
		return response, &graphTransportError{isRequestBuilt: true}
	}
	return response, nil
}

func (client *Client) receipt(call sdkgo.Call, requestID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderRequestID: requestID, ObservedAt: client.now().UTC(),
	}
}

// workbookURL addresses a workbook by drive and item ID, which covers OneDrive and SharePoint.
func workbookURL(driveID string, workbookID string) string {
	return graphBaseURL + "/drives/" + url.PathEscape(driveID) + "/items/" + url.PathEscape(workbookID) + "/workbook"
}

func tableURL(driveID string, workbookID string, table string) string {
	return workbookURL(driveID, workbookID) + "/tables/" + url.PathEscape(table)
}

// rangeURL addresses an A1 range that validateA1Range has already restricted to letters, digits, and one colon.
func rangeURL(driveID string, workbookID string, worksheet string, address string) string {
	return workbookURL(driveID, workbookID) + "/worksheets/" + url.PathEscape(worksheet) + "/range(address='" + address + "')"
}

// classifyGraphError follows Microsoft's Excel error handling order: the first
// known second-level or top-level code wins, then the HTTP status.
func classifyGraphError(status int, tokens []string) graphErrorClass {
	for _, token := range tokens {
		if class, isKnown := excelErrorClasses[strings.ToLower(token)]; isKnown {
			return class
		}
	}
	switch {
	case status == http.StatusTooManyRequests, status == http.StatusRequestTimeout,
		status == http.StatusInternalServerError, status == http.StatusBadGateway,
		status == http.StatusServiceUnavailable, status == http.StatusGatewayTimeout:
		return graphErrorRetryable
	case status == http.StatusNotFound:
		return graphErrorNotFound
	case status == http.StatusRequestEntityTooLarge:
		return graphErrorTooLarge
	default:
		return graphErrorRejected
	}
}

func statusFailureKind(status int, tokens []string) sdkgo.FailureKind {
	switch {
	case status == http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case status == http.StatusTooManyRequests || hasErrorToken(tokens, "toomanyrequestsuncategorized", "activitylimitreached"):
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

func hasErrorToken(tokens []string, lowercaseCodes ...string) bool {
	for _, token := range tokens {
		for _, code := range lowercaseCodes {
			if strings.EqualFold(token, code) {
				return true
			}
		}
	}
	return false
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

func isSuccessStatus(status int) bool { return status >= 200 && status < 300 }

func excelFailure(kind sdkgo.FailureKind, operationID string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operationID, Message: message}
}

func excelFailurePointer(kind sdkgo.FailureKind, operationID string, message string) *sdkgo.Failure {
	failure := excelFailure(kind, operationID, message)
	return &failure
}

// graphRequestID keeps Graph's request-id only when it has the documented GUID shape.
func graphRequestID(header http.Header) string {
	if value := header.Get(requestIDHeader); requestIDPattern.MatchString(value) {
		return value
	}
	return ""
}
