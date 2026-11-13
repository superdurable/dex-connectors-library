// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package infiniai

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const modelListEndpoint = "https://cloud.infini-ai.com/maas/v1/models"

// ListModelsInput has no caller-supplied filters.
type ListModelsInput struct{}

// ModelInfo contains the documented model identifier, type, and token limits.
type ModelInfo struct {
	// ID is the model identifier used in GenStudio API calls.
	ID string `json:"id"`
	// ModelType is the provider's model category.
	ModelType string `json:"modelType"`
	// ContextLength is the maximum context token count; zero means unavailable or inapplicable.
	ContextLength int64 `json:"contextLength"`
	// MaxOutputLength is the maximum output token count; zero means unavailable or inapplicable.
	MaxOutputLength int64 `json:"maxOutputLength"`
}

// ListModelsOutput contains all models currently available to the tenant.
type ListModelsOutput struct {
	// Models is ordered as returned by GenStudio.
	Models []ModelInfo `json:"models"`
}

// ListModelsOperation reads the tenant model catalog.
type ListModelsOperation struct{ client *Client }

// Definition returns the generated list-models contract.
func (ListModelsOperation) Definition() sdkgo.QueryDefinition { return ListModelsDefinition }

// Invoke reads the model catalog without modifying provider state.
func (operation ListModelsOperation) Invoke(call sdkgo.Call, _ ListModelsInput) sdkgo.QueryAttempt[ListModelsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return modelsBranch(ListModelsBranchDefect, sdkgo.FailureAuthentication, "GenStudio API key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, modelListEndpoint, nil)
	if err != nil {
		return modelsBranch(ListModelsBranchDefect, sdkgo.FailureValidation, "model list request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return modelsRetry(sdkgo.FailureAvailability, "GenStudio request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return modelsRetry(sdkgo.FailureRateLimit, "GenStudio rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return modelsRetry(sdkgo.FailureAvailability, "GenStudio is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyModelStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return modelsBranch(ListModelsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "GenStudio response exceeded the configured limit")
	}
	if err != nil {
		return modelsRetry(sdkgo.FailureAvailability, "GenStudio response was interrupted", 0)
	}
	var wire struct {
		Object string `json:"object"`
		Data   []struct {
			ID              string  `json:"id"`
			Object          string  `json:"object"`
			Created         *int64  `json:"created"`
			OwnedBy         *string `json:"owned_by"`
			ModelType       string  `json:"model_type"`
			ContextLength   *int64  `json:"context_length"`
			MaxOutputLength *int64  `json:"max_output_length"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Object != "list" || wire.Data == nil {
		return modelsBranch(ListModelsBranchInvalidResponse, sdkgo.FailureProtocol, "GenStudio returned invalid model list")
	}
	output := ListModelsOutput{Models: make([]ModelInfo, 0, len(wire.Data))}
	for _, item := range wire.Data {
		if item.ID == "" || item.Object != "model" || item.Created == nil || item.OwnedBy == nil || item.ModelType == "" || item.ContextLength == nil || item.MaxOutputLength == nil || *item.ContextLength < 0 || *item.MaxOutputLength < 0 || len(item.ID) > 512 || len(item.ModelType) > 128 || strings.Contains(item.ID, credentials.APIKey.Reveal()) || strings.Contains(item.ModelType, credentials.APIKey.Reveal()) {
			return modelsBranch(ListModelsBranchInvalidResponse, sdkgo.FailureProtocol, "GenStudio returned invalid model data")
		}
		output.Models = append(output.Models, ModelInfo{ID: item.ID, ModelType: item.ModelType, ContextLength: *item.ContextLength, MaxOutputLength: *item.MaxOutputLength})
	}
	return sdkgo.NewQueryBranch(ListModelsBranchListed, output, nil, sdkgo.Receipt{})
}
func classifyModelStatus(status int) sdkgo.QueryAttempt[ListModelsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return modelsBranch(ListModelsBranchProviderRejected, kind, "GenStudio rejected the request with HTTP "+strconv.Itoa(status))
}
func modelsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "infiniai", Operation: "listModels", Message: message}
	return sdkgo.NewQueryBranch(branch, ListModelsOutput{}, &failure, sdkgo.Receipt{})
}
func modelsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "infiniai", Operation: "listModels", Message: message}
	return sdkgo.NewQueryRetry[ListModelsOutput](failure, delay)
}
