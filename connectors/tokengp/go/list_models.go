// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tokengp

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

// ListModelsInput has no arguments; the connection identifies the accessible catalog.
type ListModelsInput struct{}

// Model identifies a model accessible to the connected TokenGP API key.
type Model struct {
	// ID is the provider's model identifier.
	ID string `json:"id"`
}

// ListModelsOutput contains only accessible model IDs, excluding pricing and account data.
type ListModelsOutput struct {
	// Models are the provider's accessible models in returned order.
	Models []Model `json:"models"`
}

// ListModelsOperation reads the TokenGP model catalog.
type ListModelsOperation struct{ client *Client }

// Definition returns the generated model-list contract.
func (ListModelsOperation) Definition() sdkgo.QueryDefinition { return ListModelsDefinition }

// Invoke lists model IDs and classifies provider and transport outcomes.
func (operation ListModelsOperation) Invoke(call sdkgo.Call, _ ListModelsInput) sdkgo.QueryAttempt[ListModelsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return modelCatalogBranch(ListModelsBranchDefect, sdkgo.FailureAuthentication, "connection API key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v1/models", nil)
	if err != nil {
		return modelCatalogBranch(ListModelsBranchDefect, sdkgo.FailureValidation, "model-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return modelCatalogRetry(sdkgo.FailureAvailability, "TokenGP request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return modelCatalogRetry(sdkgo.FailureRateLimit, "TokenGP rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return modelCatalogRetry(sdkgo.FailureAvailability, "TokenGP is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return modelCatalogBranch(ListModelsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "TokenGP response exceeded the configured limit")
	}
	if err != nil {
		return modelCatalogRetry(sdkgo.FailureAvailability, "TokenGP response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return modelCatalogBranch(ListModelsBranchProviderRejected, kind, "TokenGP rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Data == nil || len(wire.Data) > 10000 {
		return modelCatalogBranch(ListModelsBranchInvalidResponse, sdkgo.FailureProtocol, "TokenGP returned invalid model-list data")
	}
	models := make([]Model, 0, len(wire.Data))
	for _, model := range wire.Data {
		if model.ID == "" || len(model.ID) > 256 || strings.TrimSpace(model.ID) != model.ID {
			return modelCatalogBranch(ListModelsBranchInvalidResponse, sdkgo.FailureProtocol, "TokenGP returned an invalid model ID")
		}
		models = append(models, Model{ID: model.ID})
	}
	return sdkgo.NewQueryBranch(ListModelsBranchListed, ListModelsOutput{Models: models}, nil, sdkgo.Receipt{})
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
func modelCatalogBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tokengp", Operation: "listModels", Message: message}
	return sdkgo.NewQueryBranch(branch, ListModelsOutput{}, &failure, sdkgo.Receipt{})
}
func modelCatalogRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tokengp", Operation: "listModels", Message: message}
	return sdkgo.NewQueryRetry[ListModelsOutput](failure, delay)
}
