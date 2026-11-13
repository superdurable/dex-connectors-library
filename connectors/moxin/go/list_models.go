// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package moxin

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

// ListModelsInput requires no selector; the token and group define visibility.
type ListModelsInput struct{}

// Model identifies a model currently available to the configured token.
type Model struct {
	// ID is the model identifier accepted by Moxin generation APIs.
	ID string `json:"id"`
}

// ModelPage contains the current authorized model IDs.
type ModelPage struct {
	// Models may be empty when no model is assigned to the token's group.
	Models []Model `json:"models"`
}

// ListModelsOperation reads authorized model metadata.
type ListModelsOperation struct{ client *Client }

// Definition returns the generated list-models contract.
func (ListModelsOperation) Definition() sdkgo.QueryDefinition { return ListModelsDefinition }

// Invoke queries Moxin and classifies provider, protocol, and transport outcomes.
func (operation ListModelsOperation) Invoke(call sdkgo.Call, _ ListModelsInput) sdkgo.QueryAttempt[ModelPage] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validPlatformToken(credentials.PlatformToken.Reveal()) {
		return modelBranch(ListModelsBranchDefect, sdkgo.FailureAuthentication, "Moxin platform token is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v1/models", nil)
	if err != nil {
		return modelBranch(ListModelsBranchDefect, sdkgo.FailureValidation, "model-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.PlatformToken.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return modelRetry(sdkgo.FailureAvailability, "Moxin model-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return modelRetry(sdkgo.FailureRateLimit, "Moxin rate limited the model-list request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return modelRetry(sdkgo.FailureAvailability, "Moxin model-list service is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return modelBranch(ListModelsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Moxin model-list response exceeded the configured limit")
	}
	if err != nil {
		return modelRetry(sdkgo.FailureAvailability, "Moxin model-list response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return modelBranch(ListModelsBranchProviderRejected, classifyModelHTTP(response.StatusCode), "Moxin rejected the model-list query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	page, err := decodeModelPage(body)
	if err != nil {
		return modelBranch(ListModelsBranchInvalidResponse, sdkgo.FailureProtocol, "Moxin returned an invalid model-list response")
	}
	return sdkgo.NewQueryBranch(ListModelsBranchListed, page, nil, sdkgo.Receipt{})
}
func decodeModelPage(body []byte) (ModelPage, error) {
	var wire struct {
		Object string `json:"object"`
		Data   []struct {
			ID     string `json:"id"`
			Object string `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Object != "list" || wire.Data == nil || len(wire.Data) > 1000 {
		return ModelPage{}, errors.New("invalid Moxin model list")
	}
	page := ModelPage{Models: make([]Model, 0, len(wire.Data))}
	for _, item := range wire.Data {
		if item.ID == "" || item.Object != "model" {
			return ModelPage{}, errors.New("invalid Moxin model")
		}
		page.Models = append(page.Models, Model{ID: item.ID})
	}
	return page, nil
}
func validPlatformToken(token string) bool {
	if token == "" || len(token) > 2048 || strings.TrimSpace(token) != token || strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return false
	}
	for _, character := range token {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyModelHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func modelBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ModelPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "moxin", Operation: "listModels", Message: message}
	return sdkgo.NewQueryBranch(branch, ModelPage{}, &failure, sdkgo.Receipt{})
}
func modelRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ModelPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "moxin", Operation: "listModels", Message: message}
	return sdkgo.NewQueryRetry[ModelPage](failure, delay)
}
