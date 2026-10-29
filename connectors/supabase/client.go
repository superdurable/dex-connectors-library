// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package supabase provides typed Dex Steps for the Supabase REST API.
package supabase

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var columnList = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(,[A-Za-z_][A-Za-z0-9_]*)*$`)
var objectID = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// Option supplies a non-serializable client dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport for testing or custom networking.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP settings and a credential source. It supports concurrent calls.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// SelectRowsInput reads a bounded PostgREST table page.
type SelectRowsInput struct {
	// Table is an exposed table name.
	Table string `json:"table"`
	// Columns is a comma-separated PostgREST select expression; blank selects all columns.
	Columns string `json:"columns,omitempty"`
	// Limit caps rows, from 1 through 1000; zero selects 100.
	Limit int `json:"limit,omitempty"`
	// Offset skips this many rows.
	Offset int `json:"offset,omitempty"`
}

// SelectRowsOutput preserves the selected JSON row array.
type SelectRowsOutput struct {
	// Data is the JSON array of selected rows.
	Data json.RawMessage `json:"data"`
}

// UpsertRowsInput replaces rows on a database-enforced unique conflict key.
type UpsertRowsInput struct {
	// Table is an exposed table name.
	Table string `json:"table"`
	// OnConflict is a comma-separated list of unique columns; the database must enforce uniqueness.
	OnConflict string `json:"onConflict"`
	// Rows contain complete replacement values for all fields the caller owns.
	Rows []map[string]any `json:"rows"`
}

// UpsertRowsOutput preserves returned PostgREST rows.
type UpsertRowsOutput struct {
	// Data is the JSON row array returned after the merge.
	Data json.RawMessage `json:"data"`
}

// New validates configuration without making a network request. Credentials resolve on every operation call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("supabase credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("supabase base URL: %w", err)
	}
	client := &Client{baseURL: strings.TrimRight(baseURL, "/"), maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

type providerResponse struct {
	body       []byte
	branch     sdkgo.BranchID
	failure    *sdkgo.Failure
	retry      bool
	retryAfter time.Duration
}

func (client *Client) send(call sdkgo.Call, operationID, method, path string, query url.Values, body []byte, hasNotFound bool, successStatus int) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return providerResponse{branch: sdkgo.DefectBranchID, failure: failure(operationID, sdkgo.FailureAuthentication, "connection credentials are unavailable")}
	}
	token := credentials.APIKey.Reveal()
	if token == "" || !providerhttp.IsHeaderSafeCredential(token) {
		return providerResponse{branch: sdkgo.DefectBranchID, failure: failure(operationID, sdkgo.FailureAuthentication, "connection credential is invalid")}
	}
	address := client.baseURL + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(call.Context, method, address, bytes.NewReader(body))
	if err != nil {
		return providerResponse{branch: sdkgo.DefectBranchID, failure: failure(operationID, sdkgo.FailureLocalDefect, "request could not be built")}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "superdurable-supabase/0.1")
	request.Header.Set("apikey", token)
	if method == http.MethodPost {
		request.Header.Set("Prefer", "resolution=merge-duplicates,return=representation")
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{retry: true, failure: failure(operationID, sdkgo.FailureAvailability, "provider request failed")}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return providerResponse{retry: true, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), failure: failure(operationID, kind, "provider is temporarily unavailable")}
	}
	if response.StatusCode == http.StatusNotFound && hasNotFound {
		return providerResponse{branch: "notFound", failure: failure(operationID, sdkgo.FailureNotFound, "resource was not found")}
	}
	if response.StatusCode != successStatus && !(operationID == "upsertRows" && response.StatusCode == http.StatusOK) {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		} else if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		} else if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnprocessableEntity {
			kind = sdkgo.FailureValidation
		}
		return providerResponse{branch: "providerRejected", failure: failure(operationID, kind, "provider rejected the request with HTTP "+strconv.Itoa(response.StatusCode))}
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return providerResponse{branch: "invalidResponse", failure: failure(operationID, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")}
	}
	if err != nil {
		return providerResponse{retry: true, failure: failure(operationID, sdkgo.FailureAvailability, "provider response was interrupted")}
	}
	if !json.Valid(responseBody) {
		return providerResponse{branch: "invalidResponse", failure: failure(operationID, sdkgo.FailureProtocol, "provider returned malformed JSON")}
	}
	return providerResponse{body: responseBody}
}
func failure(operationID string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "supabase", Operation: operationID, Message: message}
}
func hasResponseShape(body []byte, key string) bool {
	if key == "array" {
		var values []json.RawMessage
		return json.Unmarshal(body, &values) == nil && values != nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil {
		return false
	}
	value, ok := object[key]
	if !ok || len(value) == 0 || string(value) == "null" {
		return false
	}
	switch key {
	case "items", "matches":
		var entries []json.RawMessage
		return json.Unmarshal(value, &entries) == nil && entries != nil
	case "id":
		var id string
		return json.Unmarshal(value, &id) == nil && id != ""
	case "sys":
		var system map[string]json.RawMessage
		return json.Unmarshal(value, &system) == nil && system != nil
	default:
		return true
	}
}

// SelectRowsOperation calls one Supabase endpoint.
type SelectRowsOperation struct{ client *Client }

// SelectRows returns this client's typed operation.
func (client *Client) SelectRows() SelectRowsOperation { return SelectRowsOperation{client} }

// Definition returns the generated operation contract.
func (SelectRowsOperation) Definition() sdkgo.QueryDefinition { return SelectRowsDefinition }

// Invoke validates input, sends one request, and classifies the response without exposing provider text.
func (operation SelectRowsOperation) Invoke(call sdkgo.Call, input SelectRowsInput) sdkgo.QueryAttempt[SelectRowsOutput] {
	path, query, body, err := encodeSelectRowsInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(SelectRowsBranchDefect, SelectRowsOutput{}, failure("selectRows", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "selectRows", http.MethodGet, path, query, body, false, http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[SelectRowsOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, SelectRowsOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "array") {
		return sdkgo.NewQueryBranch(SelectRowsBranchInvalidResponse, SelectRowsOutput{}, failure("selectRows", sdkgo.FailureProtocol, "provider returned an incomplete response"), sdkgo.Receipt{})
	}
	output := SelectRowsOutput{Data: json.RawMessage(response.body)}
	return sdkgo.NewQueryBranch(SelectRowsBranchSelected, output, nil, sdkgo.Receipt{})
}
func encodeSelectRowsInput(input SelectRowsInput) (string, url.Values, []byte, error) {
	if !identifier.MatchString(input.Table) || input.Limit < 0 || input.Limit > 1000 || input.Offset < 0 {
		return "", nil, nil, fmt.Errorf("invalid table, limit, or offset")
	}
	if input.Columns != "" && !columnList.MatchString(input.Columns) {
		return "", nil, nil, fmt.Errorf("columns must be comma-separated identifiers")
	}
	q := url.Values{}
	if input.Columns == "" {
		q.Set("select", "*")
	} else {
		q.Set("select", input.Columns)
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	q.Set("limit", strconv.Itoa(limit))
	if input.Offset > 0 {
		q.Set("offset", strconv.Itoa(input.Offset))
	}
	return "/" + input.Table, q, nil, nil
}

// UpsertRowsOperation calls one Supabase endpoint.
type UpsertRowsOperation struct{ client *Client }

// UpsertRows returns this client's typed operation.
func (client *Client) UpsertRows() UpsertRowsOperation { return UpsertRowsOperation{client} }

// Definition returns the generated operation contract.
func (UpsertRowsOperation) Definition() sdkgo.MutationDefinition { return UpsertRowsDefinition }

// IdempotencyKey uses the stable Dex call identity; the provider write is an upsert by stable resource key.
func (UpsertRowsOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertRowsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke validates input, sends one request, and classifies the response without exposing provider text.
func (operation UpsertRowsOperation) Invoke(call sdkgo.Call, input UpsertRowsInput) sdkgo.MutationAttempt[UpsertRowsOutput] {
	path, query, body, err := encodeUpsertRowsInput(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpsertRowsBranchDefect, UpsertRowsOutput{}, failure("upsertRows", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "upsertRows", http.MethodPost, path, query, body, false, http.StatusCreated)
	if response.retry {
		return sdkgo.NewMutationRetry[UpsertRowsOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewMutationBranch(response.branch, UpsertRowsOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "array") {
		return sdkgo.NewMutationBranch(UpsertRowsBranchInvalidResponse, UpsertRowsOutput{}, failure("upsertRows", sdkgo.FailureProtocol, "provider returned an incomplete response"), sdkgo.Receipt{})
	}
	output := UpsertRowsOutput{Data: json.RawMessage(response.body)}
	return sdkgo.NewMutationBranch(UpsertRowsBranchUpserted, output, nil, sdkgo.Receipt{})
}
func encodeUpsertRowsInput(input UpsertRowsInput) (string, url.Values, []byte, error) {
	if !identifier.MatchString(input.Table) || !columnList.MatchString(input.OnConflict) || len(input.Rows) == 0 || len(input.Rows) > 100 {
		return "", nil, nil, fmt.Errorf("table, unique onConflict, and 1 through 100 rows are required")
	}
	conflictColumns := strings.Split(input.OnConflict, ",")
	seenKeys := make(map[string]struct{}, len(input.Rows))
	for _, row := range input.Rows {
		if len(row) == 0 {
			return "", nil, nil, fmt.Errorf("rows must not be empty")
		}
		keyValues := make([]any, 0, len(conflictColumns))
		for _, column := range conflictColumns {
			value, exists := row[column]
			if !exists || value == nil {
				return "", nil, nil, fmt.Errorf("every row must contain non-null onConflict columns")
			}
			keyValues = append(keyValues, value)
		}
		key, err := json.Marshal(keyValues)
		if err != nil {
			return "", nil, nil, fmt.Errorf("onConflict values must be JSON serializable")
		}
		if _, duplicate := seenKeys[string(key)]; duplicate {
			return "", nil, nil, fmt.Errorf("rows contain duplicate onConflict values")
		}
		seenKeys[string(key)] = struct{}{}
	}
	body, err := json.Marshal(input.Rows)
	q := url.Values{"on_conflict": {input.OnConflict}}
	return "/" + input.Table, q, body, err
}
