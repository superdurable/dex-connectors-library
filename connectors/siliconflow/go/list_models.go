// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package siliconflow

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strconv"
	"time"
)

// ListModelsInput requests the catalog available to the connection's API key.
type ListModelsInput struct{}

// Model is one SiliconFlow catalog entry.
type Model struct {
	// ID is the exact model name to pass to a SiliconFlow inference request.
	ID string `json:"id"`
	// Object is the provider's object discriminator, normally "model".
	Object string `json:"object,omitempty"`
}

// ListModelsOutput contains the current model catalog for the API key.
type ListModelsOutput struct {
	// Models contains zero or more entries and does not imply inference permission for each model.
	Models []Model `json:"models"`
}

// ListModelsOperation reads the model catalog.
type ListModelsOperation struct{ client *Client }

// Definition returns the generated model-catalog contract.
func (ListModelsOperation) Definition() sdkgo.QueryDefinition { return ListModelsDefinition }

// Invoke lists models and classifies provider and transport outcomes.
func (operation ListModelsOperation) Invoke(call sdkgo.Call, _ ListModelsInput) sdkgo.QueryAttempt[ListModelsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return listModelsBranch(ListModelsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v1/models", nil)
	if err != nil {
		return listModelsBranch(ListModelsBranchDefect, sdkgo.FailureValidation, "model-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return listModelsRetry(sdkgo.FailureAvailability, "SiliconFlow request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return listModelsRetry(sdkgo.FailureRateLimit, "SiliconFlow rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return listModelsRetry(sdkgo.FailureAvailability, "SiliconFlow is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return listModelsBranch(ListModelsBranchProviderRejected, kind, "SiliconFlow rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return listModelsBranch(ListModelsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "SiliconFlow response exceeded the configured limit")
	}
	if err != nil {
		return listModelsRetry(sdkgo.FailureAvailability, "SiliconFlow response was interrupted", 0)
	}
	output, err := decodeListModelsResponse(body)
	if err != nil {
		return listModelsBranch(ListModelsBranchInvalidResponse, sdkgo.FailureProtocol, "SiliconFlow returned an invalid model list")
	}
	return sdkgo.NewQueryBranch(ListModelsBranchListed, output, nil, sdkgo.Receipt{})
}
func decodeListModelsResponse(body []byte) (ListModelsOutput, error) {
	var wire struct {
		Data []Model `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Data == nil {
		return ListModelsOutput{}, errors.New("invalid SiliconFlow model envelope")
	}
	if len(wire.Data) > 10000 {
		return ListModelsOutput{}, errors.New("SiliconFlow model count exceeds limit")
	}
	seen := make(map[string]struct{}, len(wire.Data))
	for _, model := range wire.Data {
		if model.ID == "" || len(model.ID) > 512 {
			return ListModelsOutput{}, errors.New("invalid SiliconFlow model ID")
		}
		if _, duplicate := seen[model.ID]; duplicate {
			return ListModelsOutput{}, errors.New("duplicate SiliconFlow model ID")
		}
		seen[model.ID] = struct{}{}
	}
	return ListModelsOutput{Models: wire.Data}, nil
}
func listModelsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "siliconflow", Operation: "listModels", Message: message}
	return sdkgo.NewQueryBranch(branch, ListModelsOutput{Models: []Model{}}, &failure, sdkgo.Receipt{})
}
func listModelsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "siliconflow", Operation: "listModels", Message: message}
	return sdkgo.NewQueryRetry[ListModelsOutput](failure, delay)
}
