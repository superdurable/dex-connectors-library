// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package coda provides typed Dex Steps for the Coda REST API.
package coda

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

// ListRowsInput selects a Coda table page.
type ListRowsInput struct {
	// DocID is the ID from the document URL.
	DocID string `json:"docId"`
	// TableID is the stable table ID, such as grid-abc.
	TableID string `json:"tableId"`
	// Limit caps rows at 500; zero uses Coda's default.
	Limit int `json:"limit,omitempty"`
	// PageToken is the opaque nextPageToken from a prior page.
	PageToken string `json:"pageToken,omitempty"`
}

// ListRowsOutput preserves the Coda row page.
type ListRowsOutput struct {
	// Data contains items and a possible nextPageToken.
	Data json.RawMessage `json:"data"`
}

// GetRowInput selects one Coda row.
type GetRowInput struct {
	// DocID is the document ID.
	DocID string `json:"docId"`
	// TableID is the stable table ID.
	TableID string `json:"tableId"`
	// RowID is the stable row ID, such as i-abc.
	RowID string `json:"rowId"`
}

// GetRowOutput preserves one Coda row.
type GetRowOutput struct {
	// Data is the row JSON object.
	Data json.RawMessage `json:"data"`
}

// New validates configuration without making a network request. Credentials resolve on every operation call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("coda credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("coda base URL: %w", err)
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
	token := credentials.APIToken.Reveal()
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
	request.Header.Set("User-Agent", "superdurable-coda/0.1")
	request.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{retry: true, failure: failure(operationID, sdkgo.FailureAvailability, "provider request failed")}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= 500 {
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return providerResponse{retry: true, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), failure: failure(operationID, kind, "provider is temporarily unavailable")}
	}
	if response.StatusCode == http.StatusNotFound && hasNotFound {
		return providerResponse{branch: "notFound", failure: failure(operationID, sdkgo.FailureNotFound, "resource was not found")}
	}
	if response.StatusCode != successStatus {
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
	return &sdkgo.Failure{Kind: kind, Provider: "coda", Operation: operationID, Message: message}
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

// ListRowsOperation calls one Coda endpoint.
type ListRowsOperation struct{ client *Client }

// ListRows returns this client's typed operation.
func (client *Client) ListRows() ListRowsOperation { return ListRowsOperation{client} }

// Definition returns the generated operation contract.
func (ListRowsOperation) Definition() sdkgo.QueryDefinition { return ListRowsDefinition }

// Invoke validates input, sends one request, and classifies the response without exposing provider text.
func (operation ListRowsOperation) Invoke(call sdkgo.Call, input ListRowsInput) sdkgo.QueryAttempt[ListRowsOutput] {
	path, query, body, err := encodeListRowsInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListRowsBranchDefect, ListRowsOutput{}, failure("listRows", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "listRows", http.MethodGet, path, query, body, false, http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[ListRowsOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, ListRowsOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "items") {
		return sdkgo.NewQueryBranch(ListRowsBranchInvalidResponse, ListRowsOutput{}, failure("listRows", sdkgo.FailureProtocol, "provider returned an incomplete response"), sdkgo.Receipt{})
	}
	output := ListRowsOutput{Data: json.RawMessage(response.body)}
	return sdkgo.NewQueryBranch(ListRowsBranchListed, output, nil, sdkgo.Receipt{})
}
func encodeListRowsInput(input ListRowsInput) (string, url.Values, []byte, error) {
	if !identifier.MatchString(input.DocID) || !identifier.MatchString(input.TableID) || input.Limit < 0 || input.Limit > 500 {
		return "", nil, nil, fmt.Errorf("invalid doc, table, or limit")
	}
	q := url.Values{}
	if input.Limit > 0 {
		q.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.PageToken != "" {
		q.Set("pageToken", input.PageToken)
	}
	return "/docs/" + input.DocID + "/tables/" + input.TableID + "/rows", q, nil, nil
}

// GetRowOperation calls one Coda endpoint.
type GetRowOperation struct{ client *Client }

// GetRow returns this client's typed operation.
func (client *Client) GetRow() GetRowOperation { return GetRowOperation{client} }

// Definition returns the generated operation contract.
func (GetRowOperation) Definition() sdkgo.QueryDefinition { return GetRowDefinition }

// Invoke validates input, sends one request, and classifies the response without exposing provider text.
func (operation GetRowOperation) Invoke(call sdkgo.Call, input GetRowInput) sdkgo.QueryAttempt[GetRowOutput] {
	path, query, body, err := encodeGetRowInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetRowBranchDefect, GetRowOutput{}, failure("getRow", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "getRow", http.MethodGet, path, query, body, true, http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[GetRowOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, GetRowOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "id") {
		return sdkgo.NewQueryBranch(GetRowBranchInvalidResponse, GetRowOutput{}, failure("getRow", sdkgo.FailureProtocol, "provider returned an incomplete response"), sdkgo.Receipt{})
	}
	output := GetRowOutput{Data: json.RawMessage(response.body)}
	return sdkgo.NewQueryBranch(GetRowBranchFound, output, nil, sdkgo.Receipt{})
}
func encodeGetRowInput(input GetRowInput) (string, url.Values, []byte, error) {
	if !identifier.MatchString(input.DocID) || !identifier.MatchString(input.TableID) || !identifier.MatchString(input.RowID) {
		return "", nil, nil, fmt.Errorf("doc, table, and row IDs are required")
	}
	return "/docs/" + input.DocID + "/tables/" + input.TableID + "/rows/" + input.RowID, nil, nil, nil
}
