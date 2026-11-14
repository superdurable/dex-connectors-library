// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package opencsg

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

// ListGatewayModelsInput requests models visible to the community gateway Key.
type ListGatewayModelsInput struct{}

// ListGatewayModelsOutput contains the provider count and at most fifty model IDs.
type ListGatewayModelsOutput struct {
	// Total is the number of models returned by the provider.
	Total int `json:"total"`
	// ModelIDs are the first fifty nonsecret model identifiers.
	ModelIDs []string `json:"modelIDs"`
}

// ListGatewayModelsOperation reads the model IDs accessible to an OpenCSG gateway Key.
type ListGatewayModelsOperation struct{ client *Client }

// Definition returns the generated model-list contract.
func (ListGatewayModelsOperation) Definition() sdkgo.QueryDefinition {
	return ListGatewayModelsDefinition
}

// Invoke lists model IDs without calling inference or returning model metadata.
func (operation ListGatewayModelsOperation) Invoke(call sdkgo.Call, _ ListGatewayModelsInput) sdkgo.QueryAttempt[ListGatewayModelsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return modelsBranch(ListGatewayModelsBranchDefect, sdkgo.FailureAuthentication, "OpenCSG API Key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, gatewayModelListEndpoint, nil)
	if err != nil {
		return modelsBranch(ListGatewayModelsBranchDefect, sdkgo.FailureValidation, "OpenCSG model request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return modelsRetry(sdkgo.FailureAvailability, "OpenCSG model request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return modelsRetry(sdkgo.FailureRateLimit, "OpenCSG rate limited the model request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return modelsRetry(sdkgo.FailureAvailability, "OpenCSG gateway is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyModelsHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return modelsBranch(ListGatewayModelsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "OpenCSG response exceeded the configured limit")
	}
	if err != nil {
		return modelsRetry(sdkgo.FailureAvailability, "OpenCSG response was interrupted", 0)
	}
	var wire struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Data == nil {
		return modelsBranch(ListGatewayModelsBranchInvalidResponse, sdkgo.FailureProtocol, "OpenCSG returned invalid model data")
	}
	modelIDs := make([]string, 0, min(len(wire.Data), 50))
	for _, model := range wire.Data {
		if model.ID == "" || len(model.ID) > 256 || strings.ContainsAny(model.ID, "\r\n\x00") {
			return modelsBranch(ListGatewayModelsBranchInvalidResponse, sdkgo.FailureProtocol, "OpenCSG returned invalid model ID")
		}
		if len(modelIDs) < 50 {
			modelIDs = append(modelIDs, model.ID)
		}
	}
	return sdkgo.NewQueryBranch(ListGatewayModelsBranchFound, ListGatewayModelsOutput{Total: len(wire.Data), ModelIDs: modelIDs}, nil, sdkgo.Receipt{})
}
func classifyModelsHTTPStatus(status int) sdkgo.QueryAttempt[ListGatewayModelsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return modelsBranch(ListGatewayModelsBranchProviderRejected, kind, "OpenCSG rejected model request with HTTP "+strconv.Itoa(status))
}
func modelsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListGatewayModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "opencsg", Operation: "listGatewayModels", Message: message}
	return sdkgo.NewQueryBranch(branch, ListGatewayModelsOutput{}, &failure, sdkgo.Receipt{})
}
func modelsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListGatewayModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "opencsg", Operation: "listGatewayModels", Message: message}
	return sdkgo.NewQueryRetry[ListGatewayModelsOutput](failure, delay)
}
