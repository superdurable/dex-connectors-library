// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flurouter

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

// ListModelsInput has no input because the API Key determines visible models.
type ListModelsInput struct{}

// ListModelsOutput contains only model IDs visible to the connected Key.
type ListModelsOutput struct {
	// ModelIDs are the model identifiers to use in later inference requests.
	ModelIDs []string `json:"modelIDs"`
}

// ListModelsOperation reads the Key-visible model catalog.
type ListModelsOperation struct{ client *Client }

// Definition returns the generated model-list contract.
func (ListModelsOperation) Definition() sdkgo.QueryDefinition { return ListModelsDefinition }

// Invoke reads model IDs without exposing provider messages or credentials.
func (operation ListModelsOperation) Invoke(call sdkgo.Call, _ ListModelsInput) sdkgo.QueryAttempt[ListModelsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return modelsBranch(ListModelsBranchDefect, sdkgo.FailureAuthentication, "FluRouter API Key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, modelsEndpoint, nil)
	if err != nil {
		return modelsBranch(ListModelsBranchDefect, sdkgo.FailureValidation, "FluRouter models request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return modelsRetry(sdkgo.FailureAvailability, "FluRouter models request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return modelsRetry(sdkgo.FailureRateLimit, "FluRouter rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return modelsRetry(sdkgo.FailureAvailability, "FluRouter models service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyModelsHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return modelsBranch(ListModelsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "FluRouter response exceeded the configured limit")
	}
	if err != nil {
		return modelsRetry(sdkgo.FailureAvailability, "FluRouter response was interrupted", 0)
	}
	var wire struct {
		Object string `json:"object"`
		Data   []struct {
			ID     string `json:"id"`
			Object string `json:"object"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Object != "list" || wire.Data == nil || len(wire.Data) > 1000 {
		return modelsBranch(ListModelsBranchInvalidResponse, sdkgo.FailureProtocol, "FluRouter returned invalid model data")
	}
	modelIDs := make([]string, 0, len(wire.Data))
	seen := make(map[string]struct{}, len(wire.Data))
	for _, model := range wire.Data {
		if !isValidModelID(model.ID) || model.Object != "model" {
			return modelsBranch(ListModelsBranchInvalidResponse, sdkgo.FailureProtocol, "FluRouter returned invalid model data")
		}
		if _, exists := seen[model.ID]; exists {
			return modelsBranch(ListModelsBranchInvalidResponse, sdkgo.FailureProtocol, "FluRouter returned duplicate model IDs")
		}
		seen[model.ID] = struct{}{}
		modelIDs = append(modelIDs, model.ID)
	}
	return sdkgo.NewQueryBranch(ListModelsBranchFound, ListModelsOutput{ModelIDs: modelIDs}, nil, sdkgo.Receipt{})
}

func isValidModelID(value string) bool {
	if value == "" || len(value) > 256 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func classifyModelsHTTPStatus(status int) sdkgo.QueryAttempt[ListModelsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return modelsBranch(ListModelsBranchProviderRejected, kind, "FluRouter rejected models request with HTTP "+strconv.Itoa(status))
}
func modelsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "flurouter", Operation: "listModels", Message: message}
	return sdkgo.NewQueryBranch(branch, ListModelsOutput{}, &failure, sdkgo.Receipt{})
}
func modelsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "flurouter", Operation: "listModels", Message: message}
	return sdkgo.NewQueryRetry[ListModelsOutput](failure, delay)
}
