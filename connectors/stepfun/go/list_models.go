// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stepfun

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListModelsInput has no user-supplied query parameters.
type ListModelsInput struct{}

// Model contains the non-secret fields documented for a StepFun model.
type Model struct {
	// ID is the unique model identifier.
	ID string `json:"id"`
	// CreatedAtUnix is the model creation time in Unix seconds.
	CreatedAtUnix int64 `json:"createdAtUnix"`
	// OwnedBy names the model's organization.
	OwnedBy string `json:"ownedBy"`
}

// ListModelsOutput contains the available model list.
type ListModelsOutput struct {
	// Models is the available model set; an empty set is valid.
	Models []Model `json:"models"`
}

// ListModelsOperation queries the fixed HTTPS model-list endpoint.
type ListModelsOperation struct{ client *Client }

// Definition returns the generated model-list contract.
func (ListModelsOperation) Definition() sdkgo.QueryDefinition { return ListModelsDefinition }

// Invoke lists models without persisting or logging the API key.
func (operation ListModelsOperation) Invoke(call sdkgo.Call, _ ListModelsInput) sdkgo.QueryAttempt[ListModelsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return modelBranch(ListModelsBranchDefect, sdkgo.FailureAuthentication, "API key is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, modelsEndpoint, nil)
	if err != nil {
		return modelBranch(ListModelsBranchDefect, sdkgo.FailureValidation, "model-list request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return modelRetry(sdkgo.FailureAvailability, "StepFun model-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return modelRetry(sdkgo.FailureRateLimit, "StepFun rate limited the model-list request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return modelRetry(sdkgo.FailureAvailability, "StepFun model list is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyModelStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return modelBranch(ListModelsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "StepFun response exceeded the configured limit")
	}
	if err != nil {
		return modelRetry(sdkgo.FailureAvailability, "StepFun model-list response was interrupted", 0)
	}
	var wire struct {
		Object string `json:"object"`
		Data   *[]struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created *int64 `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Object != "list" || wire.Data == nil {
		return modelBranch(ListModelsBranchInvalidResponse, sdkgo.FailureProtocol, "StepFun returned invalid model data")
	}
	output := ListModelsOutput{Models: make([]Model, 0, len(*wire.Data))}
	for _, model := range *wire.Data {
		if model.ID == "" || model.Object != "model" || model.Created == nil || *model.Created < 0 || model.OwnedBy == "" {
			return modelBranch(ListModelsBranchInvalidResponse, sdkgo.FailureProtocol, "StepFun returned invalid model metadata")
		}
		output.Models = append(output.Models, Model{ID: model.ID, CreatedAtUnix: *model.Created, OwnedBy: model.OwnedBy})
	}
	return sdkgo.NewQueryBranch(ListModelsBranchFound, output, nil, sdkgo.Receipt{})
}
func classifyModelStatus(status int) sdkgo.QueryAttempt[ListModelsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return modelBranch(ListModelsBranchProviderRejected, kind, "StepFun rejected the model-list request with HTTP "+strconv.Itoa(status))
}
func modelBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "stepfun", Operation: "listModels", Message: message}
	return sdkgo.NewQueryBranch(branch, ListModelsOutput{}, &failure, sdkgo.Receipt{})
}
func modelRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListModelsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "stepfun", Operation: "listModels", Message: message}
	return sdkgo.NewQueryRetry[ListModelsOutput](failure, delay)
}
