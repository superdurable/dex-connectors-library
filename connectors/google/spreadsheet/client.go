// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package spreadsheet implements bounded Google Sheets operations.
package spreadsheet

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
}

// WithHTTPClient overrides the default HTTP client; the caller retains ownership.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

func withClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// Client executes authenticated Google Sheets requests for connector operations.
type Client struct {
	endpoint         *url.URL
	httpClient       *http.Client
	credentials      CredentialSource
	refreshDriver    sdkgo.CredentialRefreshDriver[Credentials]
	maxResponseBytes int64
	maxRows          int
	now              func() time.Time
}

// GetValuesInput contains the provider request fields for get values.
type GetValuesInput struct {
	// SpreadsheetID is the Google Sheets spreadsheet identifier.
	SpreadsheetID string `json:"spreadsheetId"`
	// Range is an A1 notation range.
	Range string `json:"range"`
}

// GetValuesOutput contains the provider response fields for get values.
type GetValuesOutput struct {
	// Range is an A1 notation range.
	Range string `json:"range"`
	// MajorDimension is the major dimension returned by Google Sheets.
	MajorDimension string `json:"majorDimension"`
	// Values is the values returned by Google Sheets.
	Values [][]string `json:"values"`
}

// FindRowInput contains the provider request fields for find row.
type FindRowInput struct {
	// SpreadsheetID is the Google Sheets spreadsheet identifier.
	SpreadsheetID string `json:"spreadsheetId"`
	// SheetName is the worksheet title.
	SheetName string `json:"sheetName"`
	// KeyColumn specifies key column for find row input.
	KeyColumn string `json:"keyColumn"`
	// KeyValue specifies key value for find row input.
	KeyValue string `json:"keyValue"`
}

// FindRowOutput contains the provider response fields for find row.
type FindRowOutput struct {
	// RowNumber is the one-based sheet row number.
	RowNumber int64 `json:"rowNumber,omitempty"`
	// Values is the values returned by Google Sheets.
	Values map[string]string `json:"values,omitempty"`
	// ConflictingRows is the conflicting rows returned by Google Sheets.
	ConflictingRows []int64 `json:"conflictingRows,omitempty"`
}

// UpsertRowInput contains the provider request fields for upsert row.
type UpsertRowInput struct {
	// SpreadsheetID is the Google Sheets spreadsheet identifier.
	SpreadsheetID string `json:"spreadsheetId"`
	// SheetName is the worksheet title.
	SheetName string `json:"sheetName"`
	// KeyColumn specifies key column for upsert row input.
	KeyColumn string `json:"keyColumn"`
	// KeyValue specifies key value for upsert row input.
	KeyValue string `json:"keyValue"`
	// Values specifies values for upsert row input.
	Values map[string]string `json:"values"`
}

// UpsertRowOutput contains the provider response fields for upsert row.
type UpsertRowOutput struct {
	// Action is the action returned by Google Sheets.
	Action string `json:"action"`
	// RowNumber is the one-based sheet row number.
	RowNumber int64 `json:"rowNumber"`
	// UpdatedRange is the updated range returned by Google Sheets.
	UpdatedRange string `json:"updatedRange"`
}

// GetValuesOperation implements the get values connector operation.
type GetValuesOperation struct{ client *Client }

// FindRowOperation implements the find row connector operation.
type FindRowOperation struct{ client *Client }

// UpsertRowOperation implements the upsert row connector operation.
type UpsertRowOperation struct{ client *Client }

type valuesResponse struct {
	Range          string  `json:"range"`
	MajorDimension string  `json:"majorDimension"`
	Values         [][]any `json:"values"`
}

type updateResponse struct {
	UpdatedRange string `json:"updatedRange"`
	Updates      *struct {
		UpdatedRange string `json:"updatedRange"`
	} `json:"updates,omitempty"`
}

type requestResult struct {
	status    int
	header    http.Header
	body      []byte
	requestID string
}

type providerRequestError struct {
	kind         sdkgo.FailureKind
	message      string
	undispatched bool
}

type writeDispatchObservation struct {
	mutex                 sync.Mutex
	hasObtainedConnection bool
	hasFailedToConnect    bool
}

// Error returns the safe human-readable failure message.
func (failure *providerRequestError) Error() string { return failure.message }

// New validates configuration and constructs an authenticated Google Sheets client.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Hostname() == "" {
		return nil, fmt.Errorf("Google Sheets endpoint must be absolute")
	}
	if endpoint.Scheme != "https" && endpoint.Hostname() != "localhost" && endpoint.Hostname() != "127.0.0.1" {
		return nil, fmt.Errorf("Google Sheets endpoint must use HTTPS")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Google Sheets connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.httpClient == nil {
		dependencies.httpClient = &http.Client{Timeout: 25 * time.Second}
	}
	if config.MaxResponseBytes < 1 || config.MaxRows < 2 {
		return nil, fmt.Errorf("Google Sheets response and row limits must be positive")
	}
	return &Client{
		endpoint: endpoint, httpClient: dependencies.httpClient, credentials: credentials,
		refreshDriver:    NewCredentialRefreshDriver(dependencies.httpClient),
		maxResponseBytes: config.MaxResponseBytes, maxRows: int(config.MaxRows), now: dependencies.now,
	}, nil
}

// GetValues returns the GetValues operation bound to this client.
func (client *Client) GetValues() GetValuesOperation { return GetValuesOperation{client: client} }

// FindRow returns the FindRow operation bound to this client.
func (client *Client) FindRow() FindRowOperation { return FindRowOperation{client: client} }

// UpsertRow returns the UpsertRow operation bound to this client.
func (client *Client) UpsertRow() UpsertRowOperation { return UpsertRowOperation{client: client} }

// Definition returns the immutable connector operation definition.
func (GetValuesOperation) Definition() sdkgo.QueryDefinition { return GetValuesDefinition }

// Invoke executes one provider call and classifies its attempt.
func (operation GetValuesOperation) Invoke(call sdkgo.Call, input GetValuesInput) sdkgo.QueryAttempt[GetValuesOutput] {
	if strings.TrimSpace(input.SpreadsheetID) == "" || strings.TrimSpace(input.Range) == "" {
		return sdkgo.NewQueryBranch(GetValuesBranchDefect, GetValuesOutput{}, failurePointer(sdkgo.FailureValidation, "getValues", "spreadsheet ID and range are required"), sdkgo.Receipt{})
	}
	credential, failure, _ := operation.client.resolveCredential(call, "getValues")
	if failure != nil {
		return sdkgo.NewQueryBranch(GetValuesBranchDefect, GetValuesOutput{}, failure, sdkgo.Receipt{})
	}
	result, _, err := operation.client.getValues(call.Context, call, credential, input.SpreadsheetID, input.Range)
	if err != nil {
		if requestFailure, ok := err.(*providerRequestError); ok {
			switch requestFailure.kind {
			case sdkgo.FailureResponseTooLarge:
				return sdkgo.NewQueryBranch(GetValuesBranchInvalidResponse, GetValuesOutput{}, failurePointer(requestFailure.kind, "getValues", requestFailure.message), operation.client.receipt(call, result, ""))
			case sdkgo.FailureLocalDefect:
				return sdkgo.NewQueryBranch(GetValuesBranchDefect, GetValuesOutput{}, failurePointer(requestFailure.kind, "getValues", requestFailure.message), sdkgo.Receipt{})
			}
		}
		return sdkgo.NewQueryRetry[GetValuesOutput](sheetFailure(sdkgo.FailureAvailability, "getValues", "provider is unavailable"), 0)
	}
	receipt := operation.client.receipt(call, result, "")
	if result.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetValuesBranchNotFound, GetValuesOutput{}, failurePointer(sdkgo.FailureNotFound, "getValues", "spreadsheet or range was not found"), receipt)
	}
	retry, delay, retryAfterErr := classifyRetryableStatus(result.status, result.header)
	if retryAfterErr != nil {
		return sdkgo.NewQueryRetry[GetValuesOutput](sheetFailure(sdkgo.FailureProtocol, "getValues", "provider returned an invalid Retry-After header"), 0)
	}
	if retry {
		return sdkgo.NewQueryRetry[GetValuesOutput](sheetFailure(statusFailureKind(result.status), "getValues", "provider temporarily rejected the query"), delay)
	}
	if result.status < 200 || result.status >= 300 {
		return sdkgo.NewQueryBranch(GetValuesBranchProviderRejected, GetValuesOutput{}, failurePointer(statusFailureKind(result.status), "getValues", "provider rejected the query"), receipt)
	}
	values, decodeFailure := operation.client.decodeValues(result.body, "getValues")
	if decodeFailure != nil {
		return sdkgo.NewQueryBranch(GetValuesBranchInvalidResponse, GetValuesOutput{}, decodeFailure, receipt)
	}
	return sdkgo.NewQueryBranch(GetValuesBranchRead, convertValues(values), nil, receipt)
}

// Definition returns the immutable connector operation definition.
func (FindRowOperation) Definition() sdkgo.QueryDefinition { return FindRowDefinition }

// Invoke executes one provider call and classifies its attempt.
func (operation FindRowOperation) Invoke(call sdkgo.Call, input FindRowInput) sdkgo.QueryAttempt[FindRowOutput] {
	if err := validateFindInput(input); err != nil {
		return sdkgo.NewQueryBranch(FindRowBranchDefect, FindRowOutput{}, failurePointer(sdkgo.FailureValidation, "findRow", err.Error()), sdkgo.Receipt{})
	}
	credential, failure, _ := operation.client.resolveCredential(call, "findRow")
	if failure != nil {
		return sdkgo.NewQueryBranch(FindRowBranchDefect, FindRowOutput{}, failure, sdkgo.Receipt{})
	}
	result, _, err := operation.client.getValues(call.Context, call, credential, input.SpreadsheetID, quoteSheet(input.SheetName))
	if err != nil {
		if requestFailure, ok := err.(*providerRequestError); ok {
			switch requestFailure.kind {
			case sdkgo.FailureResponseTooLarge:
				return sdkgo.NewQueryBranch(FindRowBranchInvalidResponse, FindRowOutput{}, failurePointer(requestFailure.kind, "findRow", requestFailure.message), operation.client.receipt(call, result, ""))
			case sdkgo.FailureLocalDefect:
				return sdkgo.NewQueryBranch(FindRowBranchDefect, FindRowOutput{}, failurePointer(requestFailure.kind, "findRow", requestFailure.message), sdkgo.Receipt{})
			}
		}
		return sdkgo.NewQueryRetry[FindRowOutput](sheetFailure(sdkgo.FailureAvailability, "findRow", "provider is unavailable"), 0)
	}
	receipt := operation.client.receipt(call, result, "")
	if result.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(FindRowBranchNotFound, FindRowOutput{}, failurePointer(sdkgo.FailureNotFound, "findRow", "spreadsheet or sheet was not found"), receipt)
	}
	retry, delay, retryAfterErr := classifyRetryableStatus(result.status, result.header)
	if retryAfterErr != nil {
		return sdkgo.NewQueryRetry[FindRowOutput](sheetFailure(sdkgo.FailureProtocol, "findRow", "provider returned an invalid Retry-After header"), 0)
	}
	if retry {
		return sdkgo.NewQueryRetry[FindRowOutput](sheetFailure(statusFailureKind(result.status), "findRow", "provider temporarily rejected the query"), delay)
	}
	if result.status < 200 || result.status >= 300 {
		return sdkgo.NewQueryBranch(FindRowBranchProviderRejected, FindRowOutput{}, failurePointer(statusFailureKind(result.status), "findRow", "provider rejected the query"), receipt)
	}
	values, decodeFailure := operation.client.decodeValues(result.body, "findRow")
	if decodeFailure != nil {
		return sdkgo.NewQueryBranch(FindRowBranchInvalidResponse, FindRowOutput{}, decodeFailure, receipt)
	}
	found, branch, findFailure := findRow(convertValues(values).Values, input.KeyColumn, input.KeyValue)
	return sdkgo.NewQueryBranch(branch, found, findFailure, receipt)
}

func (client *Client) resolveCredential(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure, bool) {
	credential, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	if err != nil {
		return Credentials{}, failurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable"), sdkgo.IsReauthorizationRequired(err)
	}
	if validateResolvedCredentials(credential) != nil {
		return Credentials{}, failurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable"), true
	}
	return credential, nil, false
}

func (client *Client) refreshRejectedWriteCredential(ctx context.Context, call sdkgo.Call, rejected Credentials) (Credentials, bool) {
	if _, supportsRefresh := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !supportsRefresh {
		return Credentials{}, false
	}
	credential, err := sdkgo.ResolveCredentialAfterRejection(ctx, client.credentials, call, client.refreshDriver)
	if err != nil || validateResolvedCredentials(credential) != nil || credential.AccessToken.Reveal() == rejected.AccessToken.Reveal() {
		return Credentials{}, false
	}
	return credential, true
}

func (client *Client) getValues(ctx context.Context, call sdkgo.Call, credential Credentials, spreadsheetID string, valueRange string) (requestResult, Credentials, error) {
	return client.request(ctx, call, credential, http.MethodGet, spreadsheetID, "/values/"+url.PathEscape(valueRange), url.Values{"majorDimension": {"ROWS"}, "valueRenderOption": {"FORMATTED_VALUE"}}, nil)
}

func (client *Client) request(ctx context.Context, call sdkgo.Call, credential Credentials, method, spreadsheetID, suffix string, query url.Values, payload any) (requestResult, Credentials, error) {
	var encodedPayload []byte
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return requestResult{}, credential, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be encoded"}
		}
		encodedPayload = encoded
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + "/spreadsheets/" + url.PathEscape(spreadsheetID) + suffix
	for attempt := 0; attempt < 2; attempt++ {
		var body io.Reader
		if encodedPayload != nil {
			body = bytes.NewReader(encodedPayload)
		}
		request, err := http.NewRequestWithContext(ctx, method, target, body)
		if err != nil {
			return requestResult{}, credential, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be built"}
		}
		request.URL.RawQuery = query.Encode()
		request.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
		if payload != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := client.httpClient.Do(request)
		if err != nil {
			return requestResult{}, credential, &providerRequestError{kind: sdkgo.FailureTransport, message: "provider request failed"}
		}
		content, readErr := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			return requestResult{}, credential, &providerRequestError{kind: sdkgo.FailureTransport, message: "provider response could not be read"}
		}
		result := requestResult{status: response.StatusCode, header: response.Header, body: content, requestID: googleRequestID(response.Header)}
		if int64(len(content)) > client.maxResponseBytes {
			result.body = nil
			return result, credential, &providerRequestError{kind: sdkgo.FailureResponseTooLarge, message: "provider response exceeds configured limit"}
		}
		if response.StatusCode != http.StatusUnauthorized || attempt != 0 {
			return result, credential, nil
		}
		if _, ok := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !ok {
			return result, credential, nil
		}
		refreshedCredential, err := sdkgo.ResolveCredentialAfterRejection(ctx, client.credentials, call, client.refreshDriver)
		if err != nil || validateResolvedCredentials(refreshedCredential) != nil {
			return result, credential, nil
		}
		credential = refreshedCredential
	}
	return requestResult{}, credential, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "authenticated request retry was exhausted"}
}

func (client *Client) writeRequest(ctx context.Context, call sdkgo.Call, credential Credentials, method, spreadsheetID, suffix string, query url.Values, payload any) (requestResult, error) {
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return requestResult{}, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "provider write could not be encoded", undispatched: true}
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + "/spreadsheets/" + url.PathEscape(spreadsheetID) + suffix
	observation := &writeDispatchObservation{}
	requestContext := httptrace.WithClientTrace(ctx, observation.clientTrace())
	request, err := http.NewRequestWithContext(requestContext, method, target, bytes.NewReader(encodedPayload))
	if err != nil {
		return requestResult{}, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "provider write could not be built", undispatched: true}
	}
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
	request.Header.Set("Content-Type", "application/json")
	// Sheets does not document deduplication for this correlation header.
	request.Header.Set("X-Idempotency-Key", string(call.IdempotencyKey))
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return requestResult{}, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "provider write could not be prepared", undispatched: true}
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return requestResult{}, &providerRequestError{kind: sdkgo.FailureTransport, message: "provider write failed", undispatched: observation.isProvablyUndispatched()}
	}
	result := requestResult{status: response.StatusCode, header: response.Header, requestID: googleRequestID(response.Header)}
	content, readErr := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		return result, &providerRequestError{kind: sdkgo.FailureTransport, message: "provider write response could not be read"}
	}
	if int64(len(content)) > client.maxResponseBytes {
		return result, &providerRequestError{kind: sdkgo.FailureResponseTooLarge, message: "provider write response exceeds configured limit"}
	}
	result.body = content
	return result, nil
}

func (observation *writeDispatchObservation) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSDone: observation.recordDNSDone, ConnectDone: observation.recordConnectDone,
		TLSHandshakeDone: observation.recordTLSHandshakeDone, GotConn: observation.recordObtainedConnection,
	}
}

func (observation *writeDispatchObservation) isProvablyUndispatched() bool {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	return observation.hasFailedToConnect && !observation.hasObtainedConnection
}

func (observation *writeDispatchObservation) recordDNSDone(info httptrace.DNSDoneInfo) {
	if info.Err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *writeDispatchObservation) recordConnectDone(_ string, _ string, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *writeDispatchObservation) recordTLSHandshakeDone(_ tls.ConnectionState, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *writeDispatchObservation) recordObtainedConnection(httptrace.GotConnInfo) {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasObtainedConnection = true
}

func (observation *writeDispatchObservation) recordConnectionFailure() {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasFailedToConnect = true
}

func validateResolvedCredentials(credentials Credentials) error {
	if credentials.AccessToken.Reveal() == "" {
		return fmt.Errorf("Google Sheets access token is required")
	}
	return nil
}

func (client *Client) decodeValues(content []byte, operation string) (valuesResponse, *sdkgo.Failure) {
	var response valuesResponse
	if err := json.Unmarshal(content, &response); err != nil {
		return valuesResponse{}, failurePointer(sdkgo.FailureProtocol, operation, "provider response is invalid")
	}
	if len(response.Values) > client.maxRows {
		return valuesResponse{}, failurePointer(sdkgo.FailureResponseTooLarge, operation, "provider response exceeds configured row limit")
	}
	return response, nil
}

func convertValues(input valuesResponse) GetValuesOutput {
	rows := make([][]string, len(input.Values))
	for rowIndex, row := range input.Values {
		rows[rowIndex] = make([]string, len(row))
		for columnIndex, value := range row {
			rows[rowIndex][columnIndex] = fmt.Sprint(value)
		}
	}
	return GetValuesOutput{Range: input.Range, MajorDimension: input.MajorDimension, Values: rows}
}

func findRow(rows [][]string, keyColumn, keyValue string) (FindRowOutput, sdkgo.BranchID, *sdkgo.Failure) {
	if len(rows) == 0 {
		return FindRowOutput{}, FindRowBranchDefect, failurePointer(sdkgo.FailureValidation, "findRow", "sheet must contain a header row")
	}
	column := -1
	for index, header := range rows[0] {
		if header == keyColumn {
			if column >= 0 {
				return FindRowOutput{}, FindRowBranchConflict, failurePointer(sdkgo.FailureConflict, "findRow", "key column header is duplicated")
			}
			column = index
		}
	}
	if column < 0 {
		return FindRowOutput{}, FindRowBranchDefect, failurePointer(sdkgo.FailureValidation, "findRow", "key column is missing")
	}
	var matches []int64
	var selected []string
	for rowIndex := 1; rowIndex < len(rows); rowIndex++ {
		if column < len(rows[rowIndex]) && rows[rowIndex][column] == keyValue {
			matches = append(matches, int64(rowIndex+1))
			selected = rows[rowIndex]
		}
	}
	if len(matches) == 0 {
		return FindRowOutput{}, FindRowBranchNotFound, nil
	}
	if len(matches) > 1 {
		return FindRowOutput{ConflictingRows: matches}, FindRowBranchConflict, failurePointer(sdkgo.FailureConflict, "findRow", "multiple rows contain the stable key")
	}
	values := map[string]string{}
	for index, header := range rows[0] {
		if index < len(selected) {
			values[header] = selected[index]
		}
	}
	return FindRowOutput{RowNumber: matches[0], Values: values}, FindRowBranchFound, nil
}

func buildRow(rows [][]string, input UpsertRowInput) ([]string, *sdkgo.Failure) {
	if len(rows) == 0 {
		return nil, failurePointer(sdkgo.FailureValidation, "upsertRow", "sheet must contain a header row")
	}
	values := make(map[string]string, len(input.Values)+1)
	for key, value := range input.Values {
		values[key] = value
	}
	if existing, ok := values[input.KeyColumn]; ok && existing != input.KeyValue {
		return nil, failurePointer(sdkgo.FailureValidation, "upsertRow", "key column value conflicts with stable key")
	}
	values[input.KeyColumn] = input.KeyValue
	headers := map[string]bool{}
	row := make([]string, len(rows[0]))
	for index, header := range rows[0] {
		if header == "" || headers[header] {
			return nil, failurePointer(sdkgo.FailureValidation, "upsertRow", "headers must be non-empty and unique")
		}
		headers[header] = true
		row[index] = values[header]
	}
	var unknown []string
	for key := range values {
		if !headers[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, failurePointer(sdkgo.FailureValidation, "upsertRow", "values contain unknown columns: "+strings.Join(unknown, ", "))
	}
	return row, nil
}

func validateFindInput(input FindRowInput) error {
	if strings.TrimSpace(input.SpreadsheetID) == "" || strings.TrimSpace(input.SheetName) == "" || strings.TrimSpace(input.KeyColumn) == "" || strings.TrimSpace(input.KeyValue) == "" {
		return fmt.Errorf("spreadsheet ID, sheet name, key column, and key value are required")
	}
	return nil
}

func quoteSheet(name string) string { return "'" + strings.ReplaceAll(name, "'", "''") + "'" }

func classifyRetryableStatus(status int, header http.Header) (bool, time.Duration, error) {
	if status != http.StatusTooManyRequests && status < 500 {
		return false, 0, nil
	}
	delay, err := retryAfterDelay(header)
	return true, delay, err
}

func retryAfterDelay(header http.Header) (time.Duration, error) {
	value := header.Get("Retry-After")
	if value == "" {
		return 0, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse Retry-After %q: %w", value, err)
	}
	if seconds < 0 {
		return 0, fmt.Errorf("parse Retry-After %q: value cannot be negative", value)
	}
	if seconds > 0 {
		return time.Duration(seconds) * time.Second, nil
	}
	return 0, nil
}

func statusFailureKind(status int) sdkgo.FailureKind {
	switch status {
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case http.StatusNotFound:
		return sdkgo.FailureNotFound
	case http.StatusConflict:
		return sdkgo.FailureConflict
	case http.StatusTooManyRequests:
		return sdkgo.FailureRateLimit
	default:
		if status >= 500 {
			return sdkgo.FailureAvailability
		}
		return sdkgo.FailureProviderRejection
	}
}

func sheetFailure(kind sdkgo.FailureKind, operation, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "google-sheets", Operation: operation, Message: message}
}

func failurePointer(kind sdkgo.FailureKind, operation, message string) *sdkgo.Failure {
	failure := sheetFailure(kind, operation, message)
	return &failure
}

func googleRequestID(header http.Header) string {
	if value := header.Get("X-Goog-Request-Id"); value != "" {
		return value
	}
	return header.Get("X-Request-Id")
}

func (client *Client) receipt(call sdkgo.Call, result requestResult, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "google-sheets", ProviderObjectID: objectID, ProviderRequestID: result.requestID, ObservedAt: client.now().UTC()}
}
