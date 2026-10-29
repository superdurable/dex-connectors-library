// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package pinecone provides typed Dex Steps for the Pinecone REST API.
package pinecone

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

// Vector is one dense Pinecone vector, replaced by ID during upsert.
type Vector struct {
	// ID is the stable vector identity.
	ID string `json:"id"`
	// Values contains the dense vector coordinates.
	Values []float32 `json:"values"`
	// Metadata contains optional filterable attributes.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// UpsertVectorsInput writes one bounded vector batch.
type UpsertVectorsInput struct {
	// Namespace selects the index namespace; blank uses Pinecone's default.
	Namespace string `json:"namespace,omitempty"`
	// Vectors are replaced by ID; each ID must be stable across retries.
	Vectors []Vector `json:"vectors"`
}

// UpsertVectorsOutput reports Pinecone's accepted vector count.
type UpsertVectorsOutput struct {
	// UpsertedCount is the number of vectors accepted.
	UpsertedCount int `json:"upsertedCount"`
}

// QueryVectorsInput searches by one dense vector.
type QueryVectorsInput struct {
	// Namespace selects the index namespace.
	Namespace string `json:"namespace,omitempty"`
	// Vector is the dense query vector.
	Vector []float32 `json:"vector"`
	// TopK is the result limit from 1 through 10000.
	TopK int `json:"topK"`
	// IncludeMetadata requests stored metadata in matches.
	IncludeMetadata bool `json:"includeMetadata,omitempty"`
}

// QueryVectorsOutput preserves Pinecone's JSON response.
type QueryVectorsOutput struct {
	// Data contains matches and the namespace, with provider fields preserved.
	Data json.RawMessage `json:"data"`
}

// New validates configuration without making a network request. Credentials resolve on every operation call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("pinecone credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("pinecone base URL: %w", err)
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
	request.Header.Set("User-Agent", "superdurable-pinecone/0.1")
	request.Header.Set("Api-Key", token)
	request.Header.Set("X-Pinecone-Api-Version", "2025-04")
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
	return &sdkgo.Failure{Kind: kind, Provider: "pinecone", Operation: operationID, Message: message}
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

// UpsertVectorsOperation calls one Pinecone endpoint.
type UpsertVectorsOperation struct{ client *Client }

// UpsertVectors returns this client's typed operation.
func (client *Client) UpsertVectors() UpsertVectorsOperation { return UpsertVectorsOperation{client} }

// Definition returns the generated operation contract.
func (UpsertVectorsOperation) Definition() sdkgo.MutationDefinition { return UpsertVectorsDefinition }

// IdempotencyKey uses the stable Dex call identity; the provider write is an upsert by stable resource key.
func (UpsertVectorsOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertVectorsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke validates input, sends one request, and classifies the response without exposing provider text.
func (operation UpsertVectorsOperation) Invoke(call sdkgo.Call, input UpsertVectorsInput) sdkgo.MutationAttempt[UpsertVectorsOutput] {
	path, query, body, err := encodeUpsertVectorsInput(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpsertVectorsBranchDefect, UpsertVectorsOutput{}, failure("upsertVectors", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "upsertVectors", http.MethodPost, path, query, body, false, http.StatusOK)
	if response.retry {
		return sdkgo.NewMutationRetry[UpsertVectorsOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewMutationBranch(response.branch, UpsertVectorsOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "upsertedCount") {
		return sdkgo.NewMutationBranch(UpsertVectorsBranchInvalidResponse, UpsertVectorsOutput{}, failure("upsertVectors", sdkgo.FailureProtocol, "provider returned an incomplete response"), sdkgo.Receipt{})
	}
	var output UpsertVectorsOutput
	if err := json.Unmarshal(response.body, &output); err != nil || output.UpsertedCount < 0 || output.UpsertedCount > len(input.Vectors) {
		return sdkgo.NewMutationBranch(UpsertVectorsBranchInvalidResponse, UpsertVectorsOutput{}, failure("upsertVectors", sdkgo.FailureProtocol, "provider returned an invalid count"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(UpsertVectorsBranchUpserted, output, nil, sdkgo.Receipt{})
}
func encodeUpsertVectorsInput(input UpsertVectorsInput) (string, url.Values, []byte, error) {
	if len(input.Vectors) == 0 || len(input.Vectors) > 1000 {
		return "", nil, nil, fmt.Errorf("vectors must contain 1 through 1000 items")
	}
	seenIDs := make(map[string]struct{}, len(input.Vectors))
	for _, vector := range input.Vectors {
		if vector.ID == "" || len(vector.Values) == 0 {
			return "", nil, nil, fmt.Errorf("each vector needs an ID and values")
		}
		if _, duplicate := seenIDs[vector.ID]; duplicate {
			return "", nil, nil, fmt.Errorf("vector IDs must be unique within the batch")
		}
		seenIDs[vector.ID] = struct{}{}
	}
	body, err := json.Marshal(input)
	return "/vectors/upsert", nil, body, err
}

// QueryVectorsOperation calls one Pinecone endpoint.
type QueryVectorsOperation struct{ client *Client }

// QueryVectors returns this client's typed operation.
func (client *Client) QueryVectors() QueryVectorsOperation { return QueryVectorsOperation{client} }

// Definition returns the generated operation contract.
func (QueryVectorsOperation) Definition() sdkgo.QueryDefinition { return QueryVectorsDefinition }

// Invoke validates input, sends one request, and classifies the response without exposing provider text.
func (operation QueryVectorsOperation) Invoke(call sdkgo.Call, input QueryVectorsInput) sdkgo.QueryAttempt[QueryVectorsOutput] {
	path, query, body, err := encodeQueryVectorsInput(input)
	if err != nil {
		return sdkgo.NewQueryBranch(QueryVectorsBranchDefect, QueryVectorsOutput{}, failure("queryVectors", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	response := operation.client.send(call, "queryVectors", http.MethodPost, path, query, body, false, http.StatusOK)
	if response.retry {
		return sdkgo.NewQueryRetry[QueryVectorsOutput](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, QueryVectorsOutput{}, response.failure, sdkgo.Receipt{})
	}
	if !hasResponseShape(response.body, "matches") {
		return sdkgo.NewQueryBranch(QueryVectorsBranchInvalidResponse, QueryVectorsOutput{}, failure("queryVectors", sdkgo.FailureProtocol, "provider returned an incomplete response"), sdkgo.Receipt{})
	}
	output := QueryVectorsOutput{Data: json.RawMessage(response.body)}
	return sdkgo.NewQueryBranch(QueryVectorsBranchQueried, output, nil, sdkgo.Receipt{})
}
func encodeQueryVectorsInput(input QueryVectorsInput) (string, url.Values, []byte, error) {
	if len(input.Vector) == 0 || input.TopK < 1 || input.TopK > 10000 {
		return "", nil, nil, fmt.Errorf("vector and topK from 1 through 10000 are required")
	}
	body, err := json.Marshal(input)
	return "/query", nil, body, err
}
