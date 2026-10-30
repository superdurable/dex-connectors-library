// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package webflow provides typed Dex Steps for the Webflow REST API.
package webflow

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

// ListCollectionItemsInput selects one staged CMS page.
type ListCollectionItemsInput struct {
	// CollectionID is the 24-character CMS collection ID.
	CollectionID string `json:"collectionId"`
	// Limit caps items at 100; zero uses the provider default.
	Limit int `json:"limit,omitempty"`
	// Offset is the zero-based page offset.
	Offset int `json:"offset,omitempty"`
}

// ListCollectionItemsOutput preserves Webflow's item page.
type ListCollectionItemsOutput struct {
	// Data contains items and pagination.
	Data json.RawMessage `json:"data"`
}

// GetCollectionItemInput selects one staged CMS item.
type GetCollectionItemInput struct {
	// CollectionID is the 24-character CMS collection ID.
	CollectionID string `json:"collectionId"`
	// ItemID is the 24-character CMS item ID.
	ItemID string `json:"itemId"`
}

// GetCollectionItemOutput preserves one Webflow item.
type GetCollectionItemOutput struct {
	// Data is the item's JSON object.
	Data json.RawMessage `json:"data"`
}

// New validates configuration without making a network request. Credentials resolve on every operation call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("webflow credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("webflow base URL: %w", err)
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
	token := credentials.SiteToken.Reveal()
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
	request.Header.Set("User-Agent", "superdurable-webflow/0.1")
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
	return &sdkgo.Failure{Kind: kind, Provider: "webflow", Operation: operationID, Message: message}
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

// ListCollectionItemsOperation calls one Webflow endpoint.
type ListCollectionItemsOperation struct{ client *Client }

// ListCollectionItems returns this client's typed operation.
func (client *Client) ListCollectionItems() ListCollectionItemsOperation {
	return ListCollectionItemsOperation{client}
}

// Definition returns the generated operation contract.
func (ListCollectionItemsOperation) Definition() sdkgo.QueryDefinition {
	return ListCollectionItemsDefinition
}

// Invoke validates input, sends one request, and classifies the response without exposing provider text.
func (operation ListCollectionItemsOperation) Invoke(call sdkgo.Call, input ListCollectionItemsInput) sdkgo.QueryAttempt[ListCollectionItemsOutput] {
	path, query, body, err := encodeListCollectionItemsInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListCollectionItemsBranchDefect, ListCollectionItemsOutput{}, failure("listCollectionItems", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "listCollectionItems", http.MethodGet, path, query, body, false, http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[ListCollectionItemsOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, ListCollectionItemsOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "items") {
		return sdkgo.NewQueryBranch(ListCollectionItemsBranchInvalidResponse, ListCollectionItemsOutput{}, failure("listCollectionItems", sdkgo.FailureProtocol, "provider returned an incomplete response"), sdkgo.Receipt{})
	}
	output := ListCollectionItemsOutput{Data: json.RawMessage(response.body)}
	return sdkgo.NewQueryBranch(ListCollectionItemsBranchListed, output, nil, sdkgo.Receipt{})
}
func encodeListCollectionItemsInput(input ListCollectionItemsInput) (string, url.Values, []byte, error) {
	if !objectID.MatchString(input.CollectionID) || input.Limit < 0 || input.Limit > 100 || input.Offset < 0 {
		return "", nil, nil, fmt.Errorf("invalid collection ID, limit, or offset")
	}
	q := url.Values{}
	if input.Limit > 0 {
		q.Set("limit", strconv.Itoa(input.Limit))
	}
	if input.Offset > 0 {
		q.Set("offset", strconv.Itoa(input.Offset))
	}
	return "/collections/" + input.CollectionID + "/items", q, nil, nil
}

// GetCollectionItemOperation calls one Webflow endpoint.
type GetCollectionItemOperation struct{ client *Client }

// GetCollectionItem returns this client's typed operation.
func (client *Client) GetCollectionItem() GetCollectionItemOperation {
	return GetCollectionItemOperation{client}
}

// Definition returns the generated operation contract.
func (GetCollectionItemOperation) Definition() sdkgo.QueryDefinition {
	return GetCollectionItemDefinition
}

// Invoke validates input, sends one request, and classifies the response without exposing provider text.
func (operation GetCollectionItemOperation) Invoke(call sdkgo.Call, input GetCollectionItemInput) sdkgo.QueryAttempt[GetCollectionItemOutput] {
	path, query, body, err := encodeGetCollectionItemInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetCollectionItemBranchDefect, GetCollectionItemOutput{}, failure("getCollectionItem", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "getCollectionItem", http.MethodGet, path, query, body, true, http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[GetCollectionItemOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, GetCollectionItemOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "id") {
		return sdkgo.NewQueryBranch(GetCollectionItemBranchInvalidResponse, GetCollectionItemOutput{}, failure("getCollectionItem", sdkgo.FailureProtocol, "provider returned an incomplete response"), sdkgo.Receipt{})
	}
	output := GetCollectionItemOutput{Data: json.RawMessage(response.body)}
	return sdkgo.NewQueryBranch(GetCollectionItemBranchFound, output, nil, sdkgo.Receipt{})
}
func encodeGetCollectionItemInput(input GetCollectionItemInput) (string, url.Values, []byte, error) {
	if !objectID.MatchString(input.CollectionID) || !objectID.MatchString(input.ItemID) {
		return "", nil, nil, fmt.Errorf("collection and item IDs must be 24 hexadecimal characters")
	}
	return "/collections/" + input.CollectionID + "/items/" + input.ItemID, nil, nil, nil
}
