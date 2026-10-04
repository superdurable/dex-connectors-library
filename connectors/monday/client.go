// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package monday connects Dex applications to monday.com work management
// boards through the monday.com platform API, a GraphQL API served at
// https://api.monday.com/v2.
//
// A Client exposes five operations. ListItems reads one bounded page of a
// board's active items with a typed column-value filter, and GetItem reads one
// item. CreateItem creates an item with typed column values,
// UpdateItemColumnValues sets typed absolute column values, and AddUpdate posts
// an update, monday.com's item comment. Every mutation sends an
// Idempotency-Key header derived from the Dex Call ID, so a retried or
// re-dispatched attempt of one Step execution receives monday.com's cached
// result instead of writing twice.
//
// Every request pins the API-Version header to APIVersion. A connection
// authenticates with a personal API token or with monday.com OAuth 2.1, whose
// expiring access tokens CredentialRefreshDriver refreshes before they expire
// and once after monday.com rejects one.
package monday

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// APIVersion is the monday.com API version that every request pins in its API-Version header.
	APIVersion = "2026-10"
	// PersonalAPITokenAuthMethodID identifies a connection that uses a user's personal API token.
	PersonalAPITokenAuthMethodID = "personal-api-token"
	// OAuthAuthMethodID identifies a connection that uses monday.com OAuth 2.1 with refresh tokens.
	OAuthAuthMethodID = "monday-oauth"

	providerName  = "monday"
	defaultAPIURL = "https://api.monday.com/v2"

	// defaultRequestTimeout leaves room inside the 30-second Execute timeout for a token refresh and one resend.
	defaultRequestTimeout = 12 * time.Second
	// minimumIdempotencyConflictDelay spaces attempts that find the first one still running, so they do not exhaust the retry policy.
	minimumIdempotencyConflictDelay = 2 * time.Second

	apiVersionHeader          = "API-Version"
	idempotencyKeyHeader      = "Idempotency-Key"
	idempotencyReplayedHeader = "Idempotency-Replayed"
	// IdempotencyReplayedReceiptKey names the Receipt metadata entry set when monday.com replayed a cached mutation result.
	IdempotencyReplayedReceiptKey = "idempotencyReplayed"
)

var (
	errorTokenPointers = []string{"/errors/0/extensions/code", "/error_code"}
	requestIDPattern   = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	errRequestNotBuilt = errors.New("monday.com request could not be built")
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	apiURL     string
	now        func() time.Time
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 12 seconds per request.
// The caller retains ownership of the client and its transport. The connector uses a copy
// that never follows redirects, so a token is never replayed to another host. The same
// client also carries OAuth token refresh requests.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithAPIURL replaces https://api.monday.com/v2 for a local monday.com-compatible fake. The
// URL must use HTTPS unless its host is loopback, and it must not carry user information, a
// query, or a fragment. Production connections leave it unset.
func WithAPIURL(apiURL string) Option {
	return func(options *clientOptions) { options.apiURL = apiURL }
}

// WithClock overrides the clock used for Receipts, Retry-After dates, and token expiry.
// Tests use it; production connections leave it unset.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// Client executes authenticated monday.com GraphQL requests for connector operations.
// A Client is safe for concurrent use by several Steps.
type Client struct {
	apiURL           string
	httpClient       *http.Client
	credentials      CredentialSource
	refreshDriver    *CredentialRefreshDriver
	maxResponseBytes int64
	now              func() time.Time
}

// graphQLRequest is one GraphQL document with its variables; mutations carry the Step's idempotency key.
type graphQLRequest struct {
	document   string
	variables  map[string]any
	isMutation bool
}

type graphQLRequestBody struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

// mondayResponse holds the safe parts of one response; data is set only for a credential-free 2xx.
type mondayResponse struct {
	statusCode int
	data       json.RawMessage
	requestID  string
	isReplayed bool
}

// exchangeOutcome is the provider-neutral meaning of one monday.com request that each operation maps to a branch.
type exchangeOutcome uint8

const (
	exchangeSucceeded exchangeOutcome = iota + 1
	exchangeNotFound
	// exchangeRetry is safe for reads and, under their idempotency key, for every mutation.
	exchangeRetry
	exchangeRejected
	exchangeInvalid
	exchangeDefect
)

// mondayExchange is one classified request; failure is set for every outcome except exchangeSucceeded.
type mondayExchange struct {
	outcome    exchangeOutcome
	response   mondayResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// graphQLErrorSummary holds only machine-readable tokens from monday.com's errors array.
type graphQLErrorSummary struct {
	code           string
	statusCode     int
	retryInSeconds int
	columnID       string
}

// New validates configuration and constructs a monday.com client.
// Credentials are resolved before every provider call, so a replaced personal token or a
// refreshed OAuth token takes effect without a restart; the response limit is startup
// configuration. Construction never contacts monday.com.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("monday.com response limit must be positive")
	}
	if credentials == nil {
		return nil, errors.New("monday.com credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("monday.com connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("monday.com connector clock is required")
	}
	apiURL := defaultAPIURL
	if dependencies.apiURL != "" {
		validated, err := providerhttp.ValidateBaseURL(dependencies.apiURL)
		if err != nil {
			return nil, fmt.Errorf("monday.com API URL: %w", err)
		}
		apiURL = validated
	}
	httpClient := providerhttp.NewProviderHTTPClient(dependencies.httpClient, defaultRequestTimeout)
	refreshDriver := NewCredentialRefreshDriver(httpClient)
	refreshDriver.now = dependencies.now
	return &Client{
		apiURL: apiURL, httpClient: httpClient, credentials: credentials, refreshDriver: refreshDriver,
		maxResponseBytes: config.MaxResponseBytes, now: dependencies.now,
	}, nil
}

// ListItems returns the listItems Query bound to this client.
func (client *Client) ListItems() ListItemsOperation { return ListItemsOperation{client: client} }

// GetItem returns the getItem Query bound to this client.
func (client *Client) GetItem() GetItemOperation { return GetItemOperation{client: client} }

// CreateItem returns the createItem Mutation bound to this client.
func (client *Client) CreateItem() CreateItemOperation { return CreateItemOperation{client: client} }

// UpdateItemColumnValues returns the updateItemColumnValues Mutation bound to this client.
func (client *Client) UpdateItemColumnValues() UpdateItemColumnValuesOperation {
	return UpdateItemColumnValuesOperation{client: client}
}

// AddUpdate returns the addUpdate Mutation bound to this client.
func (client *Client) AddUpdate() AddUpdateOperation { return AddUpdateOperation{client: client} }

// exchange resends once after a rejected OAuth token; a 401 means monday.com ran nothing.
func (client *Client) exchange(call sdkgo.Call, operation string, request graphQLRequest) mondayExchange {
	credentials, failedResolution := client.resolveCredentials(call, operation)
	if failedResolution != nil {
		return *failedResolution
	}
	result := client.send(call, credentials, operation, request)
	if result.response.statusCode == http.StatusUnauthorized && client.canRefreshAfterRejection(credentials) {
		replacement, err := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		if err == nil && validateResolvedCredentials(replacement) == nil {
			result = client.send(call, replacement, operation, request)
		}
	}
	return result
}

// resolveCredentials separates a revoked grant from a transient refresh failure and a broken connection.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *mondayExchange) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return Credentials{}, &mondayExchange{outcome: exchangeRejected,
			failure: mondayFailure(sdkgo.FailureAuthentication, operation, "monday.com authorization must be renewed")}
	case errors.Is(err, errCredentialRefreshUnavailable):
		return Credentials{}, &mondayExchange{outcome: exchangeRetry,
			failure: mondayFailure(sdkgo.FailureAvailability, operation, "monday.com OAuth token refresh is temporarily unavailable")}
	case err != nil || validateResolvedCredentials(credentials) != nil:
		return Credentials{}, &mondayExchange{outcome: exchangeDefect,
			failure: mondayFailure(sdkgo.FailureAuthentication, operation, "monday.com connection credentials are unavailable")}
	}
	return credentials, nil
}

func (client *Client) canRefreshAfterRejection(credentials Credentials) bool {
	if credentials.AuthMethodID != OAuthAuthMethodID {
		return false
	}
	_, supportsRejectionRefresh := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials])
	return supportsRejectionRefresh
}

// send performs one HTTP exchange and classifies it without deciding retry policy.
func (client *Client) send(call sdkgo.Call, credentials Credentials, operation string, request graphQLRequest) mondayExchange {
	httpRequest, err := client.buildRequest(call, credentials, request)
	if err != nil {
		return mondayExchange{outcome: exchangeDefect, failure: mondayFailure(sdkgo.FailureLocalDefect, operation, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if isConnectionNeverEstablished(err) {
			return mondayExchange{outcome: exchangeRetry, failure: mondayFailure(sdkgo.FailureTransport, operation, "monday.com could not be reached; no request was sent")}
		}
		return mondayExchange{outcome: exchangeRetry, failure: mondayFailure(sdkgo.FailureTransport, operation, "monday.com request failed before a response arrived")}
	}
	body, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	closeErr := httpResponse.Body.Close()
	response := mondayResponse{
		statusCode: httpResponse.StatusCode,
		isReplayed: strings.EqualFold(strings.TrimSpace(httpResponse.Header.Get(idempotencyReplayedHeader)), "true"),
	}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge) && isSuccess:
		return mondayExchange{outcome: exchangeInvalid, response: response, failure: mondayFailure(sdkgo.FailureResponseTooLarge, operation, "monday.com response exceeds the configured maxResponseBytes limit")}
	case readErr != nil && !errors.Is(readErr, providerhttp.ErrBodyTooLarge), closeErr != nil:
		return mondayExchange{outcome: exchangeRetry, response: response, failure: mondayFailure(sdkgo.FailureTransport, operation, "monday.com response could not be read")}
	}
	token := credentials.authorizationToken().Reveal()
	if bytes.Contains(body, []byte(token)) {
		return mondayExchange{outcome: exchangeInvalid, response: response, failure: mondayFailure(sdkgo.FailureProtocol, operation, "monday.com response reflected the connection credential")}
	}
	response.requestID = safeRequestID(body)
	retryAfter := providerhttp.ParseRetryAfter(httpResponse.Header.Get("Retry-After"), client.now())
	if !isSuccess {
		return classifyFailedResponse(operation, response, summarizeGraphQLErrors(body, token), retryAfter)
	}
	var envelope struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return mondayExchange{outcome: exchangeInvalid, response: response, failure: mondayFailure(sdkgo.FailureProtocol, operation, "monday.com returned a response that is not a GraphQL JSON object")}
	}
	if !isJSONNull(envelope.Data) {
		response.data = envelope.Data
	}
	if len(envelope.Errors) != 0 {
		return classifyFailedResponse(operation, response, summarizeGraphQLErrors(body, token), retryAfter)
	}
	if response.data == nil {
		return mondayExchange{outcome: exchangeInvalid, response: response, failure: mondayFailure(sdkgo.FailureProtocol, operation, "monday.com returned neither data nor errors")}
	}
	return mondayExchange{outcome: exchangeSucceeded, response: response}
}

func (client *Client) buildRequest(call sdkgo.Call, credentials Credentials, request graphQLRequest) (*http.Request, error) {
	encoded, err := json.Marshal(graphQLRequestBody{Query: request.document, Variables: request.variables})
	if err != nil {
		return nil, errRequestNotBuilt
	}
	httpRequest, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.apiURL, bytes.NewReader(encoded))
	if err != nil {
		return nil, errRequestNotBuilt
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", credentials.authorizationToken().Reveal())
	httpRequest.Header.Set(apiVersionHeader, APIVersion)
	if request.isMutation {
		if call.IdempotencyKey == "" {
			return nil, errors.New("monday.com mutation has no idempotency key")
		}
		httpRequest.Header.Set(idempotencyKeyHeader, string(call.IdempotencyKey))
	}
	return httpRequest, nil
}

func (client *Client) receipt(call sdkgo.Call, response mondayResponse, objectID string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ProviderRequestID: response.requestID, ObservedAt: client.now().UTC(),
	}
	if response.isReplayed {
		receipt.Metadata = map[string]string{IdempotencyReplayedReceiptKey: "true"}
	}
	return receipt
}

// queryBranches names an operation's branches for the shared read outcome mapping.
type queryBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	invalidResponse  sdkgo.BranchID
	defect           sdkgo.BranchID
}

// queryAttemptForExchange maps every outcome except success; reads are always safe to retry.
func queryAttemptForExchange[OUT any](result mondayExchange, output OUT, receipt sdkgo.Receipt, branches queryBranches) (sdkgo.QueryAttempt[OUT], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.QueryAttempt[OUT]{}, false
	case exchangeRetry:
		return sdkgo.NewQueryRetry[OUT](result.failure, result.retryAfter), true
	case exchangeNotFound:
		return sdkgo.NewQueryBranch(branches.notFound, output, &result.failure, receipt), true
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(branches.invalidResponse, output, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewQueryBranch(branches.defect, output, &result.failure, receipt), true
	default:
		return sdkgo.NewQueryBranch(branches.providerRejected, output, &result.failure, receipt), true
	}
}

// mutationBranches names an operation's branches for the shared write outcome mapping.
type mutationBranches struct {
	notFound         sdkgo.BranchID
	providerRejected sdkgo.BranchID
	defect           sdkgo.BranchID
	// invalidResponse receives an unusable accepted response; blank selects uncertain instead.
	invalidResponse sdkgo.BranchID
}

// mutationAttemptForExchange retries an ambiguous outcome, because the retry replays under the same key.
func mutationAttemptForExchange[OUT any](result mondayExchange, requested OUT, receipt sdkgo.Receipt, branches mutationBranches) (sdkgo.MutationAttempt[OUT], bool) {
	switch result.outcome {
	case exchangeSucceeded:
		return sdkgo.MutationAttempt[OUT]{}, false
	case exchangeRetry:
		return sdkgo.NewMutationRetry[OUT](result.failure, result.retryAfter), true
	case exchangeNotFound:
		return sdkgo.NewMutationBranch(branches.notFound, requested, &result.failure, receipt), true
	case exchangeDefect:
		return sdkgo.NewMutationBranch(branches.defect, requested, &result.failure, receipt), true
	case exchangeInvalid:
		if branches.invalidResponse != "" {
			return sdkgo.NewMutationBranch(branches.invalidResponse, requested, &result.failure, receipt), true
		}
		return sdkgo.NewMutationUncertain(requested, result.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(branches.providerRejected, requested, &result.failure, receipt), true
	}
}

// decodeMutationItem reads the item a mutation field returned, even beside a nested field error.
func decodeMutationItem(data json.RawMessage, mutationField string) (Item, bool) {
	if data == nil {
		return Item{}, false
	}
	var document map[string]*itemResource
	if err := json.Unmarshal(data, &document); err != nil || document[mutationField] == nil {
		return Item{}, false
	}
	item, err := decodeItemResource(*document[mutationField])
	return item, err == nil
}

// classifyFailedResponse lets a documented error code decide before the HTTP or status_code extension status.
func classifyFailedResponse(operation string, response mondayResponse, summary graphQLErrorSummary, retryAfter time.Duration) mondayExchange {
	status := response.statusCode
	if status >= 200 && status < 300 && summary.statusCode >= 400 {
		status = summary.statusCode
	}
	if summary.retryInSeconds > 0 {
		retryAfter = max(retryAfter, min(time.Duration(summary.retryInSeconds)*time.Second, time.Hour))
	}
	describe := func(verb string) string {
		return withErrorSummary(fmt.Sprintf("monday.com %s (HTTP %d)", verb, response.statusCode), summary)
	}
	outcome := func(outcome exchangeOutcome, kind sdkgo.FailureKind, message string) mondayExchange {
		return mondayExchange{outcome: outcome, response: response, retryAfter: retryAfter, failure: mondayFailure(kind, operation, message)}
	}
	code := summary.code
	switch {
	case code == "DAILY_LIMIT_EXCEEDED":
		return outcome(exchangeRejected, sdkgo.FailureQuotaExhausted, describe("daily call limit is exhausted until midnight UTC"))
	case rateLimitErrorCodes[code]:
		return outcome(exchangeRetry, sdkgo.FailureRateLimit, describe("rate limited the request"))
	case code == "IDEMPOTENCY_CONFLICT":
		retryAfter = max(retryAfter, minimumIdempotencyConflictDelay)
		return outcome(exchangeRetry, sdkgo.FailureConflict, describe("is still processing an earlier attempt with this idempotency key"))
	case temporarilyUnavailableErrorCodes[code]:
		return outcome(exchangeRetry, sdkgo.FailureAvailability, describe("temporarily refused the request"))
	case authenticationErrorCodes[code]:
		return outcome(exchangeRejected, sdkgo.FailureAuthentication, describe("rejected the credential"))
	case authorizationErrorCodes[code]:
		return outcome(exchangeRejected, sdkgo.FailureAuthorization, describe("denied permission"))
	case notFoundErrorCodes[code]:
		return outcome(exchangeNotFound, sdkgo.FailureNotFound, describe("found no such board, group, or item"))
	case validationErrorCodes[code]:
		return outcome(exchangeRejected, sdkgo.FailureValidation, describe("rejected the input"))
	case documentErrorCodes[code]:
		return outcome(exchangeDefect, sdkgo.FailureProtocol, describe("rejected the connector's GraphQL request"))
	}
	switch {
	case status == http.StatusTooManyRequests:
		return outcome(exchangeRetry, sdkgo.FailureRateLimit, describe("rate limited the request"))
	case status == http.StatusConflict && code == "":
		retryAfter = max(retryAfter, minimumIdempotencyConflictDelay)
		return outcome(exchangeRetry, sdkgo.FailureConflict, describe("is still processing an earlier attempt with this idempotency key"))
	case status == http.StatusLocked:
		return outcome(exchangeRetry, sdkgo.FailureAvailability, describe("locked the board for a concurrent change"))
	case status == http.StatusRequestTimeout || status >= 500:
		return outcome(exchangeRetry, sdkgo.FailureAvailability, describe("could not complete the request"))
	case status == http.StatusUnauthorized:
		return outcome(exchangeRejected, sdkgo.FailureAuthentication, describe("rejected the credential"))
	case status == http.StatusForbidden:
		return outcome(exchangeRejected, sdkgo.FailureAuthorization, describe("denied permission"))
	case status == http.StatusNotFound:
		return outcome(exchangeNotFound, sdkgo.FailureNotFound, describe("found no such board, group, or item"))
	case status == http.StatusBadRequest:
		return outcome(exchangeDefect, sdkgo.FailureProtocol, describe("rejected the connector's GraphQL request"))
	case status == http.StatusUnprocessableEntity:
		return outcome(exchangeRejected, sdkgo.FailureValidation, describe("rejected the input"))
	case status >= 300 && status < 400:
		return outcome(exchangeRejected, sdkgo.FailureProtocol, describe("redirected the request"))
	default:
		return outcome(exchangeRejected, sdkgo.FailureProviderRejection, describe("rejected the request"))
	}
}

var (
	rateLimitErrorCodes = map[string]bool{
		"COMPLEXITY_BUDGET_EXHAUSTED": true, "ComplexityException": true, "RATE_LIMIT_EXCEEDED": true,
		"IP_RATE_LIMIT_EXCEEDED": true, "maxConcurrencyExceeded": true, "FIELD_LIMIT_EXCEEDED": true,
	}
	temporarilyUnavailableErrorCodes = map[string]bool{"API_TEMPORARILY_BLOCKED": true}
	authenticationErrorCodes         = map[string]bool{"Unauthorized": true, "UNAUTHORIZED": true, "NOT_AUTHENTICATED": true}
	authorizationErrorCodes          = map[string]bool{
		"UserUnauthorizedException": true, "USER_UNAUTHORIZED": true, "USER_ACCESS_DENIED": true, "missingRequiredPermissions": true,
	}
	notFoundErrorCodes = map[string]bool{
		"ResourceNotFoundException": true, "InvalidBoardIdException": true, "InvalidItemIdException": true, "InvalidGroupIdException": true,
	}
	// documentErrorCodes mean monday.com could not parse or validate the connector's own GraphQL document.
	documentErrorCodes = map[string]bool{
		"JsonParseException": true, "InvalidVersionException": true, "GRAPHQL_PARSE_FAILED": true, "GRAPHQL_VALIDATION_FAILED": true,
		"undefinedField": true, "argumentLiteralsIncompatible": true, "missingRequiredArguments": true, "variableMismatch": true,
	}
	validationErrorCodes = map[string]bool{
		"ColumnValueException": true, "CorrectedValueException": true, "InvalidColumnIdException": true, "InvalidUserIdException": true,
		"InvalidArgumentException": true, "ItemNameTooLongException": true, "RecordInvalidException": true, "ItemsLimitationException": true,
	}
)

// summarizeGraphQLErrors reads only code, status, retry, and column tokens; message text is never read.
func summarizeGraphQLErrors(body []byte, secret string) graphQLErrorSummary {
	summary := graphQLErrorSummary{}
	if tokens := providerhttp.ReadErrorTokens(body, errorTokenPointers); len(tokens) != 0 && isCredentialFreeToken(tokens[0], secret) {
		summary.code = tokens[0]
	}
	if tokens := providerhttp.ReadErrorTokens(body, []string{"/errors/0/extensions/status_code"}); len(tokens) == 1 {
		summary.statusCode, _ = strconv.Atoi(tokens[0])
	}
	if tokens := providerhttp.ReadErrorTokens(body, []string{"/errors/0/extensions/retry_in_seconds"}); len(tokens) == 1 {
		if seconds, err := strconv.ParseFloat(tokens[0], 64); err == nil && seconds > 0 && seconds <= 3600 {
			summary.retryInSeconds = int(seconds + 0.999)
		}
	}
	if tokens := providerhttp.ReadErrorTokens(body, []string{"/errors/0/extensions/error_data/column_id"}); len(tokens) == 1 && isCredentialFreeToken(tokens[0], secret) {
		summary.columnID = tokens[0]
	}
	return summary
}

func withErrorSummary(message string, summary graphQLErrorSummary) string {
	var parts []string
	if summary.code != "" {
		parts = append(parts, summary.code)
	}
	if summary.columnID != "" {
		parts = append(parts, "column "+summary.columnID)
	}
	if len(parts) == 0 {
		return message
	}
	return message + " [" + strings.Join(parts, "; ") + "]"
}

// isCredentialFreeToken rejects an error token that echoes the connection token back.
func isCredentialFreeToken(token string, secret string) bool {
	return secret == "" || !strings.Contains(token, secret)
}

// safeRequestID reads monday.com's request_id from the response extensions.
func safeRequestID(body []byte) string {
	tokens := providerhttp.ReadErrorTokens(body, []string{"/extensions/request_id"})
	if len(tokens) == 1 && requestIDPattern.MatchString(tokens[0]) {
		return tokens[0]
	}
	return ""
}

// isConnectionNeverEstablished reports a dial failure, after which monday.com cannot have received the request.
func isConnectionNeverEstablished(err error) bool {
	var operationError *net.OpError
	return errors.As(err, &operationError) && operationError.Op == "dial"
}

func isJSONNull(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

// authorizationToken returns the token the selected authentication method sends.
func (credentials Credentials) authorizationToken() sdkgo.SecretString {
	if credentials.AuthMethodID == OAuthAuthMethodID {
		return credentials.AccessToken
	}
	return credentials.APIToken
}

// validateResolvedCredentials checks only what a request needs, so credentials without renewal material pass.
func validateResolvedCredentials(credentials Credentials) error {
	switch credentials.AuthMethodID {
	case PersonalAPITokenAuthMethodID, OAuthAuthMethodID:
	default:
		return errors.New("monday.com auth_method is invalid")
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.authorizationToken().Reveal()) {
		return errors.New("monday.com token must be printable ASCII without spaces")
	}
	return nil
}

func mondayFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func mondayFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := mondayFailure(kind, operation, message)
	return &failure
}
