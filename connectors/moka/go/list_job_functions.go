// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package moka

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListJobFunctionsInput has no user-supplied query parameters.
type ListJobFunctionsInput struct{}

// JobFunction contains non-personal recruitment category metadata.
type JobFunction struct {
	// ID identifies the job function in Moka.
	ID int64 `json:"id"`
	// Label is the job function's displayed name.
	Label string `json:"label"`
	// ParentID identifies the parent function, or zero when Moka omits it.
	ParentID int64 `json:"parentId"`
	// JobCount counts this function and its descendants' jobs.
	JobCount int64 `json:"jobCount"`
}

// ListJobFunctionsOutput contains the available recruitment functions.
type ListJobFunctionsOutput struct {
	// Functions contains only job function IDs, labels, parent IDs, and counts.
	Functions []JobFunction `json:"functions"`
}

// ListJobFunctionsOperation queries the fixed HTTPS job-function endpoint.
type ListJobFunctionsOperation struct{ client *Client }

// Definition returns the generated job-function contract.
func (ListJobFunctionsOperation) Definition() sdkgo.QueryDefinition {
	return ListJobFunctionsDefinition
}

// Invoke lists job functions without storing API keys or personal details.
func (operation ListJobFunctionsOperation) Invoke(call sdkgo.Call, _ ListJobFunctionsInput) sdkgo.QueryAttempt[ListJobFunctionsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return functionBranch(ListJobFunctionsBranchDefect, sdkgo.FailureAuthentication, "Moka API key is unavailable or invalid")
	}
	endpoint := jobFunctionsEndpointPrefix + url.PathEscape(operation.client.organizationID) + "?mode=" + url.QueryEscape(operation.client.mode)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return functionBranch(ListJobFunctionsBranchDefect, sdkgo.FailureValidation, "Moka job-function request is invalid")
	}
	request.SetBasicAuth(credentials.APIKey.Reveal(), "")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return functionRetry(sdkgo.FailureAvailability, "Moka job-function request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return functionRetry(sdkgo.FailureRateLimit, "Moka rate limited the job-function request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return functionRetry(sdkgo.FailureAvailability, "Moka job functions are temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyFunctionStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return functionBranch(ListJobFunctionsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Moka response exceeded the configured limit")
	}
	if err != nil {
		return functionRetry(sdkgo.FailureAvailability, "Moka job-function response was interrupted", 0)
	}
	var wire struct {
		Jobs *[]struct {
			ID       *int64 `json:"id"`
			Label    string `json:"label"`
			ParentID *int64 `json:"parentId"`
			JobCount *int64 `json:"jobCount"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Jobs == nil {
		return functionBranch(ListJobFunctionsBranchInvalidResponse, sdkgo.FailureProtocol, "Moka returned invalid job-function data")
	}
	output := ListJobFunctionsOutput{Functions: make([]JobFunction, 0, len(*wire.Jobs))}
	for _, item := range *wire.Jobs {
		if item.ID == nil || *item.ID <= 0 || item.Label == "" || item.JobCount == nil || *item.JobCount < 0 {
			return functionBranch(ListJobFunctionsBranchInvalidResponse, sdkgo.FailureProtocol, "Moka returned invalid job-function metadata")
		}
		parentID := int64(0)
		if item.ParentID != nil {
			parentID = *item.ParentID
		}
		if parentID < 0 {
			return functionBranch(ListJobFunctionsBranchInvalidResponse, sdkgo.FailureProtocol, "Moka returned invalid parent function")
		}
		output.Functions = append(output.Functions, JobFunction{ID: *item.ID, Label: item.Label, ParentID: parentID, JobCount: *item.JobCount})
	}
	return sdkgo.NewQueryBranch(ListJobFunctionsBranchFound, output, nil, sdkgo.Receipt{})
}
func classifyFunctionStatus(status int) sdkgo.QueryAttempt[ListJobFunctionsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return functionBranch(ListJobFunctionsBranchProviderRejected, kind, "Moka rejected job-function listing with HTTP "+strconv.Itoa(status))
}
func functionBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListJobFunctionsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "moka", Operation: "listJobFunctions", Message: message}
	return sdkgo.NewQueryBranch(branch, ListJobFunctionsOutput{}, &failure, sdkgo.Receipt{})
}
func functionRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListJobFunctionsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "moka", Operation: "listJobFunctions", Message: message}
	return sdkgo.NewQueryRetry[ListJobFunctionsOutput](failure, delay)
}
