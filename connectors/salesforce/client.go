// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package salesforce implements bounded Salesforce REST API operations for
// Dex: SOQL queries with typed bind values, single-record reads, upserts keyed
// by an External ID field, and field updates by record ID.
//
// Applications use the generated operation-specific Step factories, such as
// NewQueryRecordsStep and NewUpsertRecordByExternalIDStep, with a Connection
// built by NewProjectConnection or NewConnection. The runnable example in
// examples/record-sync uses every operation in one Flow.
//
// Every request goes to the org's instance URL, which Salesforce returns with
// each access token, and runs with the object, field, and sharing access of the
// authorized user.
package salesforce

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	salesforceProviderName = "salesforce"
	defaultRequestTimeout  = 25 * time.Second
	// minimumAPIMajorVersion is the first version whose upsert response reports the created flag.
	minimumAPIMajorVersion = 46
	minimumQueryBatchSize  = 200
	maximumQueryBatchSize  = 2000
	// maxRequestBodyBytes bounds one record write, well below Salesforce's REST request limit.
	maxRequestBodyBytes = 1 << 20
	queryOptionsHeader  = "Sforce-Query-Options"
	limitInfoHeader     = "Sforce-Limit-Info"
)

var (
	apiVersionPattern = regexp.MustCompile(`^v([1-9][0-9]{1,2})\.0$`)
	// errorCodePattern accepts Salesforce's upper-snake error codes and nothing that could carry message text.
	errorCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,79}$`)
	apiUsagePattern  = regexp.MustCompile(`(?:^|,)\s*api-usage=([0-9]{1,12}/[0-9]{1,12})\s*(?:,|$)`)
)

// Salesforce error codes that the connector classifies explicitly. Every other
// code is classified by its HTTP status.
const (
	errorCodeRequestLimitExceeded = "REQUEST_LIMIT_EXCEEDED"
	errorCodeUnableToLockRow      = "UNABLE_TO_LOCK_ROW"
	errorCodeServerUnavailable    = "SERVER_UNAVAILABLE"
	errorCodeDuplicateValue       = "DUPLICATE_VALUE"
	errorCodeInvalidSessionID     = "INVALID_SESSION_ID"
	errorCodeNotFound             = "NOT_FOUND"
	errorCodeEntityIsDeleted      = "ENTITY_IS_DELETED"
)

// accessErrorCodes report a user, org, or session that cannot make the request at all.
var accessErrorCodes = map[string]bool{
	"API_CURRENTLY_DISABLED": true, "API_DISABLED_FOR_ORG": true, "FUNCTIONALITY_NOT_ENABLED": true,
	"INSUFFICIENT_ACCESS": true, "INSUFFICIENT_ACCESS_OR_READONLY": true, "INVALID_AUTH_HEADER": true,
	errorCodeInvalidSessionID: true, "NOT_API_ENABLED": true, "ORG_LOCKED": true,
}

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
}

// WithHTTPClient overrides the default 25-second HTTP client used for REST and
// token requests. The connector uses a copy that never follows redirects, so a
// session token never reaches another host; the caller retains ownership of the
// original client and its transport.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// Client executes authenticated Salesforce REST requests for connector
// operations. A Client is immutable after New and safe for concurrent use by
// Dex Workers.
type Client struct {
	apiVersion       string
	httpClient       *http.Client
	credentials      CredentialSource
	refreshDriver    sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes int64
	queryBatchSize   int
	now              func() time.Time
}

// ProviderError is one machine-readable Salesforce REST error. It keeps the
// error code and the field API names Salesforce reported, never the message
// text, which can repeat record values.
type ProviderError struct {
	// ErrorCode is Salesforce's error code, such as REQUIRED_FIELD_MISSING.
	ErrorCode string `json:"errorCode"`
	// Fields lists the field API names Salesforce associated with the error, such as LastName.
	Fields []string `json:"fields,omitempty"`
}

// salesforceRequest is one REST request relative to the org's instance URL.
// Its body is resent once after a 401 forces a credential refresh.
type salesforceRequest struct {
	method        string
	path          string
	query         url.Values
	body          []byte
	header        http.Header
	responseLimit int64
}

type salesforceResponse struct {
	status   int
	header   http.Header
	body     []byte
	apiUsage string
}

type salesforceRequestError struct {
	kind    sdkgo.FailureKind
	message string
}

// Error returns the safe human-readable failure message.
func (failure *salesforceRequestError) Error() string { return failure.message }

// failureRoute is the kind of continuation a failed provider response selects.
type failureRoute int

const (
	routeRetry failureRoute = iota + 1
	routeNotFound
	routeRequestRejected
	routeProviderRejected
	routeDefect
	routeInvalidResponse
)

// providerOutcome classifies a failed attempt before an operation maps its route to a branch.
type providerOutcome struct {
	route          failureRoute
	failure        sdkgo.Failure
	retryAfter     time.Duration
	providerErrors []ProviderError
	apiUsage       string
}

// operationBranches names the branch each failure route selects for one operation.
type operationBranches struct {
	operationID      string
	notFound         sdkgo.BranchID
	requestRejected  sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
	// isDuplicateValueRetryable treats DUPLICATE_VALUE as a concurrent attempt racing on a unique external ID.
	isDuplicateValueRetryable bool
}

// New validates configuration and constructs an authenticated Salesforce client.
// It fails when apiVersion is not a vNN.0 version from v46.0, a limit is outside
// its documented range, or credentials is nil.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !isSupportedAPIVersion(config.APIVersion) {
		return nil, fmt.Errorf("Salesforce apiVersion must be a vNN.0 version no older than v%d.0", minimumAPIMajorVersion)
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("Salesforce maxResponseBytes must be positive")
	}
	if config.QueryBatchSize < minimumQueryBatchSize || config.QueryBatchSize > maximumQueryBatchSize {
		return nil, fmt.Errorf("Salesforce queryBatchSize must be from %d to %d", minimumQueryBatchSize, maximumQueryBatchSize)
	}
	if credentials == nil {
		return nil, errors.New("credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Salesforce connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.httpClient == nil {
		dependencies.httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &Client{
		apiVersion:       config.APIVersion,
		httpClient:       providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout),
		credentials:      credentials,
		refreshDriver:    NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes: config.MaxResponseBytes,
		queryBatchSize:   int(config.QueryBatchSize),
		now:              dependencies.now,
	}, nil
}

// QueryRecords returns the QueryRecords operation bound to this client.
func (client *Client) QueryRecords() QueryRecordsOperation {
	return QueryRecordsOperation{client: client}
}

// GetRecord returns the GetRecord operation bound to this client.
func (client *Client) GetRecord() GetRecordOperation { return GetRecordOperation{client: client} }

// UpsertRecordByExternalID returns the UpsertRecordByExternalID operation bound to this client.
func (client *Client) UpsertRecordByExternalID() UpsertRecordByExternalIDOperation {
	return UpsertRecordByExternalIDOperation{client: client}
}

// UpdateRecord returns the UpdateRecord operation bound to this client.
func (client *Client) UpdateRecord() UpdateRecordOperation {
	return UpdateRecordOperation{client: client}
}

// resolveCredential selects providerRejected for a terminal refresh failure and retries any other failure.
func (client *Client) resolveCredential(call sdkgo.Call, branches operationBranches) (Credentials, *providerOutcome) {
	credential, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if sdkgo.IsReauthorizationRequired(err) {
		return Credentials{}, &providerOutcome{route: routeProviderRejected, failure: salesforceFailure(
			sdkgo.FailureAuthentication, branches.operationID, "Salesforce authorization is revoked or expired; reauthorize the connection")}
	}
	if err != nil {
		return Credentials{}, &providerOutcome{route: routeRetry, failure: salesforceFailure(
			sdkgo.FailureAuthentication, branches.operationID, "Salesforce credentials are temporarily unavailable")}
	}
	if validateResolvedCredentials(credential) != nil {
		return Credentials{}, &providerOutcome{route: routeDefect, failure: salesforceFailure(
			sdkgo.FailureAuthentication, branches.operationID, "Salesforce connection credentials are missing or invalid")}
	}
	return credential, nil
}

// send performs one request and classifies every failed response; only upsert defines 300.
func (client *Client) send(
	call sdkgo.Call,
	credential *Credentials,
	branches operationBranches,
	request salesforceRequest,
) (salesforceResponse, *providerOutcome) {
	response, err := client.sendRequest(call, credential, request)
	var requestErr *salesforceRequestError
	if errors.As(err, &requestErr) {
		switch requestErr.kind {
		case sdkgo.FailureLocalDefect:
			return response, &providerOutcome{route: routeDefect, failure: salesforceFailure(requestErr.kind, branches.operationID, requestErr.message)}
		case sdkgo.FailureResponseTooLarge:
			return response, &providerOutcome{route: routeInvalidResponse, failure: salesforceFailure(requestErr.kind, branches.operationID, requestErr.message), apiUsage: response.apiUsage}
		}
	}
	if err != nil {
		// Every operation is safe to repeat, including both writes, so an unknown outcome is retried.
		return response, &providerOutcome{route: routeRetry, failure: salesforceFailure(sdkgo.FailureTransport, branches.operationID, "Salesforce request outcome is unknown")}
	}
	if (response.status >= 200 && response.status < 300) || response.status == http.StatusMultipleChoices {
		return response, nil
	}
	outcome := client.classifyFailedResponse(branches, response)
	return response, &outcome
}

// sendRequest sends request; after one 401 it forces one coordinated refresh and sends once more.
func (client *Client) sendRequest(call sdkgo.Call, credential *Credentials, request salesforceRequest) (salesforceResponse, error) {
	for attempt := 0; attempt < 2; attempt++ {
		target, err := client.requestURL(credential.InstanceURL, request)
		if err != nil {
			return salesforceResponse{}, &salesforceRequestError{kind: sdkgo.FailureLocalDefect, message: "Salesforce request URL could not be built"}
		}
		var body io.Reader
		if request.body != nil {
			body = bytes.NewReader(request.body)
		}
		httpRequest, err := http.NewRequestWithContext(call.Context, request.method, target, body)
		if err != nil {
			return salesforceResponse{}, &salesforceRequestError{kind: sdkgo.FailureLocalDefect, message: "Salesforce request could not be built"}
		}
		for name, values := range request.header {
			httpRequest.Header[name] = values
		}
		httpRequest.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
		httpRequest.Header.Set("Accept", "application/json")
		if request.body != nil {
			httpRequest.Header.Set("Content-Type", "application/json")
		}
		httpResponse, err := client.httpClient.Do(httpRequest)
		if err != nil {
			return salesforceResponse{}, &salesforceRequestError{kind: sdkgo.FailureTransport, message: "Salesforce request failed"}
		}
		// Error bodies get their own bound, so a small success limit never hides an error status.
		content, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, max(request.responseLimit, providerhttp.MaxErrorBodyBytes))
		closeErr := httpResponse.Body.Close()
		response := salesforceResponse{
			status: httpResponse.StatusCode, header: httpResponse.Header, body: content,
			apiUsage: parseAPIUsage(httpResponse.Header.Get(limitInfoHeader)),
		}
		isSuccess := response.status >= 200 && response.status < 300
		isOversized := errors.Is(readErr, providerhttp.ErrBodyTooLarge) || (isSuccess && int64(len(content)) > request.responseLimit)
		if isOversized && isSuccess {
			response.body = nil
			return response, &salesforceRequestError{kind: sdkgo.FailureResponseTooLarge, message: "Salesforce response exceeds the configured maxResponseBytes limit"}
		}
		if isOversized {
			// An oversized error body carries no usable error code, so the status alone classifies it.
			response.body = nil
			readErr = nil
		}
		if readErr != nil || closeErr != nil {
			return response, &salesforceRequestError{kind: sdkgo.FailureTransport, message: "Salesforce response could not be read"}
		}
		if response.status != http.StatusUnauthorized || attempt != 0 {
			return response, nil
		}
		if _, ok := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !ok {
			return response, nil
		}
		// Salesforce reports an expired or revoked session as 401 INVALID_SESSION_ID before applying any change.
		refreshed, err := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		if err != nil || validateResolvedCredentials(refreshed) != nil {
			return response, nil
		}
		*credential = refreshed
	}
	return salesforceResponse{}, &salesforceRequestError{kind: sdkgo.FailureLocalDefect, message: "authenticated Salesforce request retry was exhausted"}
}

// classifyFailedResponse maps a non-2xx Salesforce response onto a route
// without retaining any provider message text.
func (client *Client) classifyFailedResponse(branches operationBranches, response salesforceResponse) providerOutcome {
	providerErrors := parseProviderErrors(response.body)
	errorCode := ""
	if len(providerErrors) > 0 {
		errorCode = providerErrors[0].ErrorCode
	}
	outcome := providerOutcome{providerErrors: providerErrors, apiUsage: response.apiUsage}
	operationID := branches.operationID
	retryAfter := providerhttp.ParseRetryAfter(response.header.Get("Retry-After"), client.now())
	switch {
	case errorCode == errorCodeRequestLimitExceeded || response.status == http.StatusTooManyRequests:
		outcome.route, outcome.retryAfter = routeRetry, retryAfter
		outcome.failure = salesforceFailure(sdkgo.FailureRateLimit, operationID, describeProviderError("Salesforce API request limit was reached", errorCode))
	case errorCode == errorCodeUnableToLockRow:
		outcome.route, outcome.retryAfter = routeRetry, retryAfter
		outcome.failure = salesforceFailure(sdkgo.FailureConflict, operationID, describeProviderError("Salesforce could not lock the record", errorCode))
	case errorCode == errorCodeDuplicateValue && branches.isDuplicateValueRetryable:
		outcome.route = routeRetry
		outcome.failure = salesforceFailure(sdkgo.FailureConflict, operationID, describeProviderError("a concurrent write holds the unique value", errorCode))
	case errorCode == errorCodeServerUnavailable || response.status >= 500:
		outcome.route, outcome.retryAfter = routeRetry, retryAfter
		outcome.failure = salesforceFailure(sdkgo.FailureAvailability, operationID, describeProviderError("Salesforce is temporarily unavailable", errorCode))
	case response.status == http.StatusUnauthorized || errorCode == errorCodeInvalidSessionID:
		outcome.route = routeProviderRejected
		outcome.failure = salesforceFailure(sdkgo.FailureAuthentication, operationID, describeProviderError("Salesforce rejected the session", errorCode))
	case accessErrorCodes[errorCode] || response.status == http.StatusForbidden:
		outcome.route = routeProviderRejected
		outcome.failure = salesforceFailure(sdkgo.FailureAuthorization, operationID, describeProviderError("Salesforce denied access", errorCode))
	case errorCode == errorCodeNotFound || errorCode == errorCodeEntityIsDeleted || response.status == http.StatusNotFound:
		outcome.route = routeNotFound
		outcome.failure = salesforceFailure(sdkgo.FailureNotFound, operationID, describeProviderError("Salesforce did not find the resource", errorCode))
	case response.status == http.StatusBadRequest || response.status == http.StatusConflict ||
		response.status == http.StatusPreconditionFailed || response.status == http.StatusUnprocessableEntity:
		outcome.route = routeRequestRejected
		outcome.failure = salesforceFailure(sdkgo.FailureValidation, operationID, describeProviderError("Salesforce rejected the request", errorCode))
	default:
		outcome.route = routeProviderRejected
		outcome.failure = salesforceFailure(sdkgo.FailureProviderRejection, operationID, describeProviderError("Salesforce rejected the request", errorCode))
	}
	return outcome
}

// branch returns the branch outcome selects, or "" for Retry.
func (branches operationBranches) branch(route failureRoute) sdkgo.BranchID {
	switch route {
	case routeNotFound:
		return branches.notFound
	case routeRequestRejected:
		return branches.requestRejected
	case routeProviderRejected:
		return branches.providerRejected
	case routeInvalidResponse:
		return branches.invalidResponse
	case routeDefect:
		return branches.defect
	default:
		return ""
	}
}

func (client *Client) receipt(call sdkgo.Call, objectID string, apiUsage string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: salesforceProviderName,
		ProviderObjectID: objectID, ObservedAt: client.now().UTC(),
	}
	if apiUsage != "" {
		receipt.Metadata = map[string]string{"apiUsage": apiUsage}
	}
	return receipt
}

// requestURL joins the validated instance URL, the versioned REST path, and query.
func (client *Client) requestURL(instanceURL string, request salesforceRequest) (string, error) {
	base, err := providerhttp.ValidateBaseURL(instanceURL)
	if err != nil {
		return "", err
	}
	target := base + request.path
	if len(request.query) > 0 {
		target += "?" + request.query.Encode()
	}
	return target, nil
}

func (client *Client) dataPath(segments ...string) string {
	escaped := make([]string, 0, len(segments)+2)
	escaped = append(escaped, "services", "data", client.apiVersion)
	for _, segment := range segments {
		escaped = append(escaped, url.PathEscape(segment))
	}
	return "/" + strings.Join(escaped, "/")
}

// queryAttemptFromOutcome converts a failed read into a Query Retry or branch.
func queryAttemptFromOutcome[T any](client *Client, call sdkgo.Call, branches operationBranches, outcome *providerOutcome) sdkgo.QueryAttempt[T] {
	branch := branches.branch(outcome.route)
	if branch == "" {
		return sdkgo.NewQueryRetry[T](outcome.failure, outcome.retryAfter)
	}
	var zero T
	failure := outcome.failure
	return sdkgo.NewQueryBranch(branch, zero, &failure, client.receipt(call, "", outcome.apiUsage))
}

// mutationAttemptFromOutcome converts a failed write into a Mutation Retry or branch carrying value.
func mutationAttemptFromOutcome[T any](
	client *Client,
	call sdkgo.Call,
	branches operationBranches,
	outcome *providerOutcome,
	value T,
) sdkgo.MutationAttempt[T] {
	branch := branches.branch(outcome.route)
	if branch == "" {
		return sdkgo.NewMutationRetry[T](outcome.failure, outcome.retryAfter)
	}
	failure := outcome.failure
	return sdkgo.NewMutationBranch(branch, value, &failure, client.receipt(call, "", outcome.apiUsage))
}

func validateResolvedCredentials(credentials Credentials) error {
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Salesforce access token is missing or invalid")
	}
	if err := validateInstanceURL(credentials.InstanceURL); err != nil {
		return err
	}
	switch credentials.AuthMethodID {
	case "", ProductionOAuthAuthMethodID, SandboxOAuthAuthMethodID, JWTBearerAuthMethodID:
		return nil
	default:
		return errors.New("Salesforce authorization method is invalid")
	}
}

// validateInstanceURL requires an HTTPS origin (loopback HTTP for tests) without a path.
func validateInstanceURL(instanceURL string) error {
	base, err := providerhttp.ValidateBaseURL(instanceURL)
	if err != nil {
		return errors.New("Salesforce instance URL is missing or invalid")
	}
	parsed, err := url.Parse(base)
	if err != nil || (parsed.Path != "" && parsed.Path != "/") {
		return errors.New("Salesforce instance URL must not contain a path")
	}
	return nil
}

// parseProviderErrors reads Salesforce's error array, keeping only codes and field names.
func parseProviderErrors(body []byte) []ProviderError {
	var decoded []struct {
		ErrorCode string   `json:"errorCode"`
		Fields    []string `json:"fields"`
	}
	if json.Unmarshal(body, &decoded) != nil {
		return nil
	}
	providerErrors := make([]ProviderError, 0, len(decoded))
	for _, item := range decoded {
		if !errorCodePattern.MatchString(item.ErrorCode) {
			continue
		}
		providerError := ProviderError{ErrorCode: item.ErrorCode}
		for _, field := range item.Fields {
			if isFieldAPIName(field) {
				providerError.Fields = append(providerError.Fields, field)
			}
		}
		providerErrors = append(providerErrors, providerError)
	}
	if len(providerErrors) == 0 {
		return nil
	}
	return providerErrors
}

// describeProviderError appends the validated error code, never provider message text.
func describeProviderError(message string, errorCode string) string {
	if errorCode == "" {
		return message
	}
	return message + " (" + errorCode + ")"
}

func parseAPIUsage(limitInfo string) string {
	match := apiUsagePattern.FindStringSubmatch(limitInfo)
	if match == nil {
		return ""
	}
	return match[1]
}

func isSupportedAPIVersion(apiVersion string) bool {
	match := apiVersionPattern.FindStringSubmatch(apiVersion)
	if match == nil {
		return false
	}
	major, err := strconv.Atoi(match[1])
	return err == nil && major >= minimumAPIMajorVersion
}

func salesforceFailure(kind sdkgo.FailureKind, operationID, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: salesforceProviderName, Operation: operationID, Message: message}
}

func salesforceFailurePointer(kind sdkgo.FailureKind, operationID, message string) *sdkgo.Failure {
	failure := salesforceFailure(kind, operationID, message)
	return &failure
}
