// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package contentful provides typed Dex Steps for the Contentful REST API.
package contentful

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

// GetEntryInput identifies one published entry.
type GetEntryInput struct {
	// SpaceID is the Contentful space ID.
	SpaceID string `json:"spaceId"`
	// EnvironmentID is the environment ID, usually master.
	EnvironmentID string `json:"environmentId"`
	// EntryID is the published entry ID.
	EntryID string `json:"entryId"`
}

// GetEntryOutput preserves the entry JSON.
type GetEntryOutput struct {
	// Data is the published entry object.
	Data json.RawMessage `json:"data"`
}

// ListEntriesInput selects one page of published entries.
type ListEntriesInput struct {
	// SpaceID is the Contentful space ID.
	SpaceID string `json:"spaceId"`
	// EnvironmentID is the environment ID.
	EnvironmentID string `json:"environmentId"`
	// Limit caps entries, from 1 through 1000; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
	// Skip is the zero-based offset.
	Skip int `json:"skip,omitempty"`
}

// ListEntriesOutput preserves the entry page JSON.
type ListEntriesOutput struct {
	// Data contains items, total, limit, and skip.
	Data json.RawMessage `json:"data"`
}

// New validates configuration without making a network request. Credentials resolve on every operation call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("contentful credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("contentful base URL: %w", err)
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
	token := credentials.DeliveryToken.Reveal()
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
	request.Header.Set("User-Agent", "superdurable-contentful/0.1")
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
	return &sdkgo.Failure{Kind: kind, Provider: "contentful", Operation: operationID, Message: message}
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

// GetEntryOperation calls one Contentful endpoint.
type GetEntryOperation struct{ client *Client }

// GetEntry returns this client's typed operation.
func (client *Client) GetEntry() GetEntryOperation { return GetEntryOperation{client} }

// Definition returns the generated operation contract.
func (GetEntryOperation) Definition() sdkgo.QueryDefinition { return GetEntryDefinition }

// Invoke validates input, sends one request, and classifies the response without exposing provider text.
func (operation GetEntryOperation) Invoke(call sdkgo.Call, input GetEntryInput) sdkgo.QueryAttempt[GetEntryOutput] {
	path, query, body, err := encodeGetEntryInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetEntryBranchDefect, GetEntryOutput{}, failure("getEntry", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "getEntry", http.MethodGet, path, query, body, true, http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[GetEntryOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, GetEntryOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "sys") {
		return sdkgo.NewQueryBranch(GetEntryBranchInvalidResponse, GetEntryOutput{}, failure("getEntry", sdkgo.FailureProtocol, "provider returned an incomplete response"), sdkgo.Receipt{})
	}
	output := GetEntryOutput{Data: json.RawMessage(response.body)}
	return sdkgo.NewQueryBranch(GetEntryBranchFound, output, nil, sdkgo.Receipt{})
}
func encodeGetEntryInput(input GetEntryInput) (string, url.Values, []byte, error) {
	if !identifier.MatchString(input.SpaceID) || !identifier.MatchString(input.EnvironmentID) || !identifier.MatchString(input.EntryID) {
		return "", nil, nil, fmt.Errorf("space, environment, and entry IDs are required")
	}
	return "/spaces/" + input.SpaceID + "/environments/" + input.EnvironmentID + "/entries/" + input.EntryID, nil, nil, nil
}

// ListEntriesOperation calls one Contentful endpoint.
type ListEntriesOperation struct{ client *Client }

// ListEntries returns this client's typed operation.
func (client *Client) ListEntries() ListEntriesOperation { return ListEntriesOperation{client} }

// Definition returns the generated operation contract.
func (ListEntriesOperation) Definition() sdkgo.QueryDefinition { return ListEntriesDefinition }

// Invoke validates input, sends one request, and classifies the response without exposing provider text.
func (operation ListEntriesOperation) Invoke(call sdkgo.Call, input ListEntriesInput) sdkgo.QueryAttempt[ListEntriesOutput] {
	path, query, body, err := encodeListEntriesInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListEntriesBranchDefect, ListEntriesOutput{}, failure("listEntries", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "listEntries", http.MethodGet, path, query, body, false, http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[ListEntriesOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, ListEntriesOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "items") {
		return sdkgo.NewQueryBranch(ListEntriesBranchInvalidResponse, ListEntriesOutput{}, failure("listEntries", sdkgo.FailureProtocol, "provider returned an incomplete response"), sdkgo.Receipt{})
	}
	output := ListEntriesOutput{Data: json.RawMessage(response.body)}
	return sdkgo.NewQueryBranch(ListEntriesBranchListed, output, nil, sdkgo.Receipt{})
}
func encodeListEntriesInput(input ListEntriesInput) (string, url.Values, []byte, error) {
	if !identifier.MatchString(input.SpaceID) || !identifier.MatchString(input.EnvironmentID) || input.Limit < 0 || input.Limit > 1000 || input.Skip < 0 {
		return "", nil, nil, fmt.Errorf("invalid space, environment, limit, or skip")
	}
	q := url.Values{}
	if input.Limit > 0 {
		q.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.Skip > 0 {
		q.Set("skip", strconv.Itoa(input.Skip))
	}
	return "/spaces/" + input.SpaceID + "/environments/" + input.EnvironmentID + "/entries", q, nil, nil
}
