// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package spreadsheet implements bounded Google Sheets operations.
package spreadsheet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
}

func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

func withClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

type Client struct {
	endpoint         *url.URL
	httpClient       *http.Client
	credentials      sdkgo.CredentialProvider[Credentials]
	maxResponseBytes int64
	maxRows          int
	now              func() time.Time
}

type GetValuesInput struct {
	SpreadsheetID string `json:"spreadsheetId"`
	Range         string `json:"range"`
}

type GetValuesOutput struct {
	Range          string     `json:"range"`
	MajorDimension string     `json:"majorDimension"`
	Values         [][]string `json:"values"`
}

type FindRowInput struct {
	SpreadsheetID string `json:"spreadsheetId"`
	SheetName     string `json:"sheetName"`
	KeyColumn     string `json:"keyColumn"`
	KeyValue      string `json:"keyValue"`
}

type FindRowOutput struct {
	RowNumber       int64             `json:"rowNumber,omitempty"`
	Values          map[string]string `json:"values,omitempty"`
	ConflictingRows []int64           `json:"conflictingRows,omitempty"`
}

type UpsertRowInput struct {
	SpreadsheetID string            `json:"spreadsheetId"`
	SheetName     string            `json:"sheetName"`
	KeyColumn     string            `json:"keyColumn"`
	KeyValue      string            `json:"keyValue"`
	Values        map[string]string `json:"values"`
}

type UpsertRowOutput struct {
	Action       string `json:"action"`
	RowNumber    int64  `json:"rowNumber"`
	UpdatedRange string `json:"updatedRange"`
}

type GetValuesOperation struct{ client *Client }
type FindRowOperation struct{ client *Client }
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
	kind    sdkgo.FailureKind
	message string
}

func (failure *providerRequestError) Error() string { return failure.message }

func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
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
		maxResponseBytes: config.MaxResponseBytes, maxRows: int(config.MaxRows), now: dependencies.now,
	}, nil
}

func (client *Client) GetValues() GetValuesOperation { return GetValuesOperation{client: client} }
func (client *Client) FindRow() FindRowOperation     { return FindRowOperation{client: client} }
func (client *Client) UpsertRow() UpsertRowOperation { return UpsertRowOperation{client: client} }

func (GetValuesOperation) Definition() sdkgo.QueryDefinition { return GetValuesDefinition }

func (operation GetValuesOperation) Invoke(call sdkgo.Call, input GetValuesInput) sdkgo.QueryAttempt[GetValuesOutput] {
	if strings.TrimSpace(input.SpreadsheetID) == "" || strings.TrimSpace(input.Range) == "" {
		return sdkgo.NewQueryBranch(GetValuesBranchDefect, GetValuesOutput{}, failurePointer(sdkgo.FailureValidation, "getValues", "spreadsheet ID and range are required"), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, "getValues")
	if failure != nil {
		return sdkgo.NewQueryBranch(GetValuesBranchDefect, GetValuesOutput{}, failure, sdkgo.Receipt{})
	}
	result, err := operation.client.getValues(call, credential, input.SpreadsheetID, input.Range)
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
	if retry, delay := retryableStatus(result.status, result.header); retry {
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

func (FindRowOperation) Definition() sdkgo.QueryDefinition { return FindRowDefinition }

func (operation FindRowOperation) Invoke(call sdkgo.Call, input FindRowInput) sdkgo.QueryAttempt[FindRowOutput] {
	if err := validateFindInput(input); err != nil {
		return sdkgo.NewQueryBranch(FindRowBranchDefect, FindRowOutput{}, failurePointer(sdkgo.FailureValidation, "findRow", err.Error()), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, "findRow")
	if failure != nil {
		return sdkgo.NewQueryBranch(FindRowBranchDefect, FindRowOutput{}, failure, sdkgo.Receipt{})
	}
	result, err := operation.client.getValues(call, credential, input.SpreadsheetID, quoteSheet(input.SheetName))
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
	if retry, delay := retryableStatus(result.status, result.header); retry {
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

func (UpsertRowOperation) Definition() sdkgo.MutationDefinition { return UpsertRowDefinition }

func (UpsertRowOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertRowInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

func (operation UpsertRowOperation) Invoke(call sdkgo.Call, input UpsertRowInput) sdkgo.MutationAttempt[UpsertRowOutput] {
	findInput := FindRowInput{SpreadsheetID: input.SpreadsheetID, SheetName: input.SheetName, KeyColumn: input.KeyColumn, KeyValue: input.KeyValue}
	if err := validateFindInput(findInput); err != nil || len(input.Values) == 0 {
		message := "values are required"
		if err != nil {
			message = err.Error()
		}
		return sdkgo.NewMutationBranch(UpsertRowBranchDefect, UpsertRowOutput{}, failurePointer(sdkgo.FailureValidation, "upsertRow", message), sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, "upsertRow")
	if failure != nil {
		return sdkgo.NewMutationBranch(UpsertRowBranchDefect, UpsertRowOutput{}, failure, sdkgo.Receipt{})
	}
	lookup, err := operation.client.getValues(call, credential, input.SpreadsheetID, quoteSheet(input.SheetName))
	if err != nil {
		if requestFailure, ok := err.(*providerRequestError); ok {
			switch requestFailure.kind {
			case sdkgo.FailureResponseTooLarge:
				return sdkgo.NewMutationBranch(UpsertRowBranchInvalidResponse, UpsertRowOutput{}, failurePointer(requestFailure.kind, "upsertRow", requestFailure.message), operation.client.receipt(call, lookup, ""))
			case sdkgo.FailureLocalDefect:
				return sdkgo.NewMutationBranch(UpsertRowBranchDefect, UpsertRowOutput{}, failurePointer(requestFailure.kind, "upsertRow", requestFailure.message), sdkgo.Receipt{})
			}
		}
		return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(sdkgo.FailureAvailability, "upsertRow", "provider is unavailable before write"), 0)
	}
	lookupReceipt := operation.client.receipt(call, lookup, "")
	if retry, delay := retryableStatus(lookup.status, lookup.header); retry {
		return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(statusFailureKind(lookup.status), "upsertRow", "provider temporarily rejected the pre-write query"), delay)
	}
	if lookup.status < 200 || lookup.status >= 300 {
		return sdkgo.NewMutationBranch(UpsertRowBranchProviderRejected, UpsertRowOutput{}, failurePointer(statusFailureKind(lookup.status), "upsertRow", "provider rejected the pre-write query"), lookupReceipt)
	}
	values, decodeFailure := operation.client.decodeValues(lookup.body, "upsertRow")
	if decodeFailure != nil {
		return sdkgo.NewMutationBranch(UpsertRowBranchInvalidResponse, UpsertRowOutput{}, decodeFailure, lookupReceipt)
	}
	rows := convertValues(values).Values
	match, branch, findFailure := findRow(rows, input.KeyColumn, input.KeyValue)
	if branch == FindRowBranchConflict {
		return sdkgo.NewMutationBranch(UpsertRowBranchConflict, UpsertRowOutput{}, findFailure, lookupReceipt)
	}
	if branch != FindRowBranchFound && branch != FindRowBranchNotFound {
		return sdkgo.NewMutationBranch(UpsertRowBranchDefect, UpsertRowOutput{}, findFailure, lookupReceipt)
	}
	row, buildFailure := buildRow(rows, input)
	if buildFailure != nil {
		return sdkgo.NewMutationBranch(UpsertRowBranchDefect, UpsertRowOutput{}, buildFailure, lookupReceipt)
	}
	action := "inserted"
	method := http.MethodPost
	targetRange := quoteSheet(input.SheetName)
	query := url.Values{"valueInputOption": {"RAW"}, "insertDataOption": {"INSERT_ROWS"}}
	pathSuffix := "/values/" + url.PathEscape(targetRange) + ":append"
	rowNumber := int64(len(rows) + 1)
	if branch == FindRowBranchFound {
		action = "updated"
		method = http.MethodPut
		rowNumber = match.RowNumber
		targetRange = fmt.Sprintf("%s!A%d", quoteSheet(input.SheetName), rowNumber)
		query = url.Values{"valueInputOption": {"RAW"}}
		pathSuffix = "/values/" + url.PathEscape(targetRange)
	}
	payload := map[string]any{"range": targetRange, "majorDimension": "ROWS", "values": [][]string{row}}
	writeResult, err := operation.client.request(call, credential, method, input.SpreadsheetID, pathSuffix, query, payload)
	if err != nil {
		if requestFailure, ok := err.(*providerRequestError); ok && requestFailure.kind == sdkgo.FailureLocalDefect {
			return sdkgo.NewMutationBranch(UpsertRowBranchDefect, UpsertRowOutput{}, failurePointer(requestFailure.kind, "upsertRow", requestFailure.message), lookupReceipt)
		}
		return sdkgo.NewMutationUncertain(UpsertRowOutput{}, sheetFailure(sdkgo.FailureTransport, "upsertRow", "provider outcome is unknown"), lookupReceipt)
	}
	receipt := operation.client.receipt(call, writeResult, fmt.Sprintf("%s#%s!%d", input.SpreadsheetID, input.SheetName, rowNumber))
	if writeResult.status == http.StatusTooManyRequests {
		delay := retryAfterDelay(writeResult.header)
		return sdkgo.NewMutationRetry[UpsertRowOutput](sheetFailure(statusFailureKind(writeResult.status), "upsertRow", "provider conclusively rejected the write temporarily"), delay)
	}
	if writeResult.status >= 500 {
		return sdkgo.NewMutationUncertain(UpsertRowOutput{}, sheetFailure(sdkgo.FailureAvailability, "upsertRow", "provider write outcome is unknown"), receipt)
	}
	if writeResult.status < 200 || writeResult.status >= 300 {
		return sdkgo.NewMutationBranch(UpsertRowBranchProviderRejected, UpsertRowOutput{}, failurePointer(statusFailureKind(writeResult.status), "upsertRow", "provider rejected the write"), receipt)
	}
	var response updateResponse
	if err := json.Unmarshal(writeResult.body, &response); err != nil {
		return sdkgo.NewMutationUncertain(UpsertRowOutput{}, sheetFailure(sdkgo.FailureProtocol, "upsertRow", "provider returned an invalid write response"), receipt)
	}
	updatedRange := response.UpdatedRange
	if response.Updates != nil && response.Updates.UpdatedRange != "" {
		updatedRange = response.Updates.UpdatedRange
	}
	return sdkgo.NewMutationBranch(UpsertRowBranchUpserted, UpsertRowOutput{Action: action, RowNumber: rowNumber, UpdatedRange: updatedRange}, nil, receipt)
}

func (client *Client) resolveCredential(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure) {
	credential, err := client.credentials.Resolve(call)
	if err != nil || credential.Validate() != nil {
		return Credentials{}, failurePointer(sdkgo.FailureAuthentication, operation, "connection credentials are unavailable")
	}
	return credential, nil
}

func (client *Client) getValues(call sdkgo.Call, credential Credentials, spreadsheetID string, valueRange string) (requestResult, error) {
	return client.request(call, credential, http.MethodGet, spreadsheetID, "/values/"+url.PathEscape(valueRange), url.Values{"majorDimension": {"ROWS"}, "valueRenderOption": {"FORMATTED_VALUE"}}, nil)
}

func (client *Client) request(call sdkgo.Call, credential Credentials, method, spreadsheetID, suffix string, query url.Values, payload any) (requestResult, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return requestResult{}, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be encoded"}
		}
		body = bytes.NewReader(encoded)
	}
	target := strings.TrimRight(client.endpoint.String(), "/") + "/spreadsheets/" + url.PathEscape(spreadsheetID) + suffix
	request, err := http.NewRequestWithContext(call.Context, method, target, body)
	if err != nil {
		return requestResult{}, &providerRequestError{kind: sdkgo.FailureLocalDefect, message: "provider request could not be built"}
	}
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return requestResult{}, &providerRequestError{kind: sdkgo.FailureTransport, message: "provider request failed"}
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil {
		return requestResult{}, &providerRequestError{kind: sdkgo.FailureTransport, message: "provider response could not be read"}
	}
	if int64(len(content)) > client.maxResponseBytes {
		return requestResult{status: response.StatusCode, header: response.Header, requestID: googleRequestID(response.Header)}, &providerRequestError{kind: sdkgo.FailureResponseTooLarge, message: "provider response exceeds configured limit"}
	}
	return requestResult{status: response.StatusCode, header: response.Header, body: content, requestID: googleRequestID(response.Header)}, nil
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

func retryableStatus(status int, header http.Header) (bool, time.Duration) {
	if status != http.StatusTooManyRequests && status < 500 {
		return false, 0
	}
	return true, retryAfterDelay(header)
}

func retryAfterDelay(header http.Header) time.Duration {
	seconds, _ := strconv.Atoi(header.Get("Retry-After"))
	if seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return 0
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
