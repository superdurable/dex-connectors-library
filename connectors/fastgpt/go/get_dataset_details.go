// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fastgpt

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetDatasetDetailsInput identifies one knowledge base visible to the personal API Key.
type GetDatasetDetailsInput struct {
	// ID is the knowledge base identifier copied from its FastGPT URL.
	ID string `json:"id"`
}

// GetDatasetDetailsOutput contains only knowledge base metadata, excluding models and content.
type GetDatasetDetailsOutput struct {
	// ID is the provider's knowledge base identifier.
	ID string `json:"id"`
	// Name is the knowledge base title.
	Name string `json:"name"`
	// Type is the provider's knowledge base type.
	Type string `json:"type"`
	// Status is the provider's knowledge base status.
	Status string `json:"status"`
}

// GetDatasetDetailsOperation reads one FastGPT knowledge base.
type GetDatasetDetailsOperation struct{ client *Client }

// Definition returns the generated knowledge-base detail contract.
func (GetDatasetDetailsOperation) Definition() sdkgo.QueryDefinition {
	return GetDatasetDetailsDefinition
}

// Invoke reads knowledge base metadata and classifies provider and transport outcomes.
func (operation GetDatasetDetailsOperation) Invoke(call sdkgo.Call, input GetDatasetDetailsInput) sdkgo.QueryAttempt[GetDatasetDetailsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return datasetDetailsBranch(GetDatasetDetailsBranchDefect, sdkgo.FailureAuthentication, "personal API Key is unavailable or invalid")
	}
	if input.ID == "" || len(input.ID) > 128 || strings.TrimSpace(input.ID) != input.ID || strings.ContainsAny(input.ID, "/?#&") {
		return datasetDetailsBranch(GetDatasetDetailsBranchDefect, sdkgo.FailureValidation, "knowledge base ID is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/api/core/dataset/detail?id="+url.QueryEscape(input.ID), nil)
	if err != nil {
		return datasetDetailsBranch(GetDatasetDetailsBranchDefect, sdkgo.FailureValidation, "knowledge base request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return datasetDetailsRetry(sdkgo.FailureAvailability, "FastGPT request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return datasetDetailsRetry(sdkgo.FailureRateLimit, "FastGPT rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return datasetDetailsRetry(sdkgo.FailureAvailability, "FastGPT is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return datasetDetailsBranch(GetDatasetDetailsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "FastGPT response exceeded the configured limit")
	}
	if err != nil {
		return datasetDetailsRetry(sdkgo.FailureAvailability, "FastGPT response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return datasetDetailsBranch(GetDatasetDetailsBranchProviderRejected, kind, "FastGPT rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			ID     string `json:"_id"`
			Name   string `json:"name"`
			Type   string `json:"type"`
			Status string `json:"status"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return datasetDetailsBranch(GetDatasetDetailsBranchInvalidResponse, sdkgo.FailureProtocol, "FastGPT returned invalid knowledge base data")
	}
	if *wire.Code != 200 {
		kind := sdkgo.FailureProviderRejection
		if *wire.Code == 401 {
			kind = sdkgo.FailureAuthentication
		}
		if *wire.Code == 403 {
			kind = sdkgo.FailureAuthorization
		}
		if *wire.Code == 429 {
			return datasetDetailsRetry(sdkgo.FailureRateLimit, "FastGPT rate limited the request", retryAfter)
		}
		if *wire.Code >= 500 {
			return datasetDetailsRetry(sdkgo.FailureAvailability, "FastGPT is temporarily unavailable", retryAfter)
		}
		return datasetDetailsBranch(GetDatasetDetailsBranchProviderRejected, kind, "FastGPT rejected the knowledge base request")
	}
	if wire.Data == nil || wire.Data.ID != input.ID || wire.Data.Name == "" || len(wire.Data.Name) > 512 || wire.Data.Type == "" || len(wire.Data.Type) > 128 || wire.Data.Status == "" || len(wire.Data.Status) > 128 {
		return datasetDetailsBranch(GetDatasetDetailsBranchInvalidResponse, sdkgo.FailureProtocol, "FastGPT returned invalid knowledge base metadata")
	}
	return sdkgo.NewQueryBranch(GetDatasetDetailsBranchFound, GetDatasetDetailsOutput{ID: wire.Data.ID, Name: wire.Data.Name, Type: wire.Data.Type, Status: wire.Data.Status}, nil, sdkgo.Receipt{})
}

func isValidAPIKey(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func datasetDetailsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetDatasetDetailsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fastgpt", Operation: "getDatasetDetails", Message: message}
	return sdkgo.NewQueryBranch(branch, GetDatasetDetailsOutput{}, &failure, sdkgo.Receipt{})
}
func datasetDetailsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetDatasetDetailsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fastgpt", Operation: "getDatasetDetails", Message: message}
	return sdkgo.NewQueryRetry[GetDatasetDetailsOutput](failure, delay)
}
