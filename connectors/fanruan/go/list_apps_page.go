// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fanruan

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListAppsPageInput selects one page of authorized applications.
type ListAppsPageInput struct {
	// Skip omits this many earlier applications, starting at zero.
	Skip int `json:"skip"`
	// Limit is the page size from 1 through 100; zero uses 100.
	Limit int `json:"limit"`
}

// AppMetadata identifies one application without exposing form records.
type AppMetadata struct {
	// ID is the stable application ID.
	ID string `json:"id"`
	// Name is the application display name.
	Name string `json:"name"`
}

// ListAppsPageOutput contains one page and a possible next offset.
type ListAppsPageOutput struct {
	// Apps are the applications visible to the API Key.
	Apps []AppMetadata `json:"apps"`
	// NextSkip is the next offset when this page is full; zero means no further page is suggested.
	NextSkip int `json:"nextSkip"`
}

// ListAppsPageOperation reads an authorized application page without modifying it.
type ListAppsPageOperation struct{ client *Client }

// Definition returns the generated application-list contract.
func (ListAppsPageOperation) Definition() sdkgo.QueryDefinition { return ListAppsPageDefinition }

// Invoke returns one page of app IDs and names, omitting all form and record data.
func (operation ListAppsPageOperation) Invoke(call sdkgo.Call, input ListAppsPageInput) sdkgo.QueryAttempt[ListAppsPageOutput] {
	if input.Skip < 0 || input.Skip > int(^uint(0)>>1)-100 || input.Limit < 0 || input.Limit > 100 {
		return appBranch(ListAppsPageBranchDefect, sdkgo.FailureValidation, "Jiandaoyun pagination is invalid")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return appBranch(ListAppsPageBranchDefect, sdkgo.FailureAuthentication, "Jiandaoyun API Key is unavailable or invalid")
	}
	body, err := json.Marshal(struct {
		Limit int `json:"limit"`
		Skip  int `json:"skip"`
	}{limit, input.Skip})
	if err != nil {
		return appBranch(ListAppsPageBranchDefect, sdkgo.FailureValidation, "Jiandaoyun pagination is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, appsEndpoint, bytes.NewReader(body))
	if err != nil {
		return appBranch(ListAppsPageBranchDefect, sdkgo.FailureValidation, "Jiandaoyun app-list request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return appRetry(sdkgo.FailureAvailability, "Jiandaoyun app-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return appRetry(sdkgo.FailureRateLimit, "Jiandaoyun rate limited the app-list request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return appRetry(sdkgo.FailureAvailability, "Jiandaoyun app list is temporarily unavailable", retryAfter)
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return appBranch(ListAppsPageBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Jiandaoyun response exceeded the configured limit")
	}
	if err != nil {
		return appRetry(sdkgo.FailureAvailability, "Jiandaoyun app-list response was interrupted", 0)
	}
	if response.StatusCode == http.StatusBadRequest {
		var providerError struct {
			Code int `json:"code"`
		}
		if json.Unmarshal(responseBody, &providerError) == nil && providerError.Code == 8303 {
			return appRetry(sdkgo.FailureRateLimit, "Jiandaoyun rate limited the app-list request", retryAfter)
		}
		return appBranch(ListAppsPageBranchProviderRejected, sdkgo.FailureProviderRejection, "Jiandaoyun rejected the app-list request")
	}
	if response.StatusCode != http.StatusOK {
		return classifyAppHTTPStatus(response.StatusCode)
	}
	var wire struct {
		Apps []struct {
			Name string `json:"name"`
			ID   string `json:"app_id"`
		} `json:"apps"`
	}
	if json.Unmarshal(responseBody, &wire) != nil || wire.Apps == nil {
		return appBranch(ListAppsPageBranchInvalidResponse, sdkgo.FailureProtocol, "Jiandaoyun returned invalid app metadata")
	}
	output := ListAppsPageOutput{Apps: make([]AppMetadata, 0, len(wire.Apps))}
	for _, app := range wire.Apps {
		if app.ID == "" || app.Name == "" {
			return appBranch(ListAppsPageBranchInvalidResponse, sdkgo.FailureProtocol, "Jiandaoyun returned incomplete app metadata")
		}
		output.Apps = append(output.Apps, AppMetadata{ID: app.ID, Name: app.Name})
	}
	if len(output.Apps) == limit {
		output.NextSkip = input.Skip + limit
	}
	return sdkgo.NewQueryBranch(ListAppsPageBranchListed, output, nil, sdkgo.Receipt{})
}
func classifyAppHTTPStatus(status int) sdkgo.QueryAttempt[ListAppsPageOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return appBranch(ListAppsPageBranchProviderRejected, kind, "Jiandaoyun rejected the app-list request with HTTP "+strconv.Itoa(status))
}
func appBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListAppsPageOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fanruan", Operation: "listAppsPage", Message: message}
	return sdkgo.NewQueryBranch(branch, ListAppsPageOutput{}, &failure, sdkgo.Receipt{})
}
func appRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListAppsPageOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fanruan", Operation: "listAppsPage", Message: message}
	return sdkgo.NewQueryRetry[ListAppsPageOutput](failure, delay)
}
