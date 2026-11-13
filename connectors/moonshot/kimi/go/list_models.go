// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kimi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListModelsInput requests models visible to the Kimi API Key.
type ListModelsInput struct{}

// ListModelsOutput contains the provider count and at most fifty model IDs.
type ListModelsOutput struct {
	// Total is the number of models returned by the provider.
	Total int `json:"total"`
	// ModelIDs are the first fifty nonsecret model identifiers.
	ModelIDs []string `json:"modelIDs"`
}

// ListModelsOperation reads model IDs accessible to a Kimi API Key.
type ListModelsOperation struct{ client *Client }

// Definition returns the generated model-list contract.
func (ListModelsOperation) Definition() sdkgo.QueryDefinition {
	return ListModelsDefinition
}

// Invoke lists model IDs without calling inference or returning model metadata.
func (operation ListModelsOperation) Invoke(call sdkgo.Call, _ ListModelsInput) sdkgo.QueryAttempt[ListModelsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return modelsBranch(ListModelsBranchDefect, sdkgo.FailureAuthentication, "Kimi API Key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, modelListEndpoint, nil)
	if err != nil {
		return modelsBranch(ListModelsBranchDefect, sdkgo.FailureValidation, "Kimi model request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return modelsRetry(sdkgo.FailureAvailability, "Kimi model request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		var providerError struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
		if readErr == nil && json.Unmarshal(body, &providerError) == nil && providerError.Error.Type == "exceeded_current_quota_error" {
			return modelsBranch(ListModelsBranchProviderRejected, sdkgo.FailureProviderRejection, "Kimi account quota is exhausted")
		}
		return modelsRetry(sdkgo.FailureRateLimit, "Kimi rate limited the model request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return modelsRetry(sdkgo.FailureAvailability, "Kimi API is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyModelsHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return modelsBranch(ListModelsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Kimi response exceeded the configured limit")
	}
	if err != nil {
		return modelsRetry(sdkgo.FailureAvailability, "Kimi response was interrupted", 0)
	}
	var wire struct {
		Object string `json:"object"`
		Data   []struct {
			ID     string `json:"id"`
			Object string `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Object != "list" || wire.Data == nil {
		return modelsBranch(ListModelsBranchInvalidResponse, sdkgo.FailureProtocol, "Kimi returned invalid model data")
	}
	modelIDs := make([]string, 0, min(len(wire.Data), 50))
	for _, model := range wire.Data {
		if model.Object != "model" || model.ID == "" || len(model.ID) > 256 || strings.ContainsAny(model.ID, "\r\n\x00") {
			return modelsBranch(ListModelsBranchInvalidResponse, sdkgo.FailureProtocol, "Kimi returned invalid model ID")
		}
		if len(modelIDs) < 50 {
			modelIDs = append(modelIDs, model.ID)
		}
	}
	return sdkgo.NewQueryBranch(ListModelsBranchFound, ListModelsOutput{Total: len(wire.Data), ModelIDs: modelIDs}, nil, sdkgo.Receipt{})
}
func classifyModelsHTTPStatus(status int) sdkgo.QueryAttempt[ListModelsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return modelsBranch(ListModelsBranchProviderRejected, kind, "Kimi rejected model request with HTTP "+strconv.Itoa(status))
}
func modelsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kimi", Operation: "listModels", Message: message}
	return sdkgo.NewQueryBranch(branch, ListModelsOutput{}, &failure, sdkgo.Receipt{})
}
func modelsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "kimi", Operation: "listModels", Message: message}
	return sdkgo.NewQueryRetry[ListModelsOutput](failure, delay)
}
