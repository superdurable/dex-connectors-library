// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tingyun

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strconv"
	"time"
)

// ListApplicationsInput is empty because Tingyun lists all visible mobile apps.
type ListApplicationsInput struct{}

// ApplicationMetadata identifies a mobile app without its secret appToken.
type ApplicationMetadata struct {
	// ID is Tingyun's numeric mobile application identifier.
	ID int64 `json:"id"`
	// Name is the mobile application display name.
	Name string `json:"name"`
}

// ListApplicationsOutput contains the mobile applications visible to the accessToken.
type ListApplicationsOutput struct {
	// Applications are the visible application identifiers and names.
	Applications []ApplicationMetadata `json:"applications"`
}

// ListApplicationsOperation reads mobile application metadata without modifying provider state.
type ListApplicationsOperation struct{ client *Client }

// Definition returns the generated application-list contract.
func (ListApplicationsOperation) Definition() sdkgo.QueryDefinition {
	return ListApplicationsDefinition
}

// Invoke reads applications and excludes secret appToken values from durable output.
func (operation ListApplicationsOperation) Invoke(call sdkgo.Call, _ ListApplicationsInput) sdkgo.QueryAttempt[ListApplicationsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAccessToken(credentials.AccessToken.Reveal()) {
		return applicationBranch(ListApplicationsBranchDefect, sdkgo.FailureAuthentication, "Tingyun accessToken is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.endpoint, nil)
	if err != nil {
		return applicationBranch(ListApplicationsBranchDefect, sdkgo.FailureValidation, "Tingyun application-list request is invalid")
	}
	request.Header.Set("Authorization", "token "+credentials.AccessToken.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return applicationRetry(sdkgo.FailureAvailability, "Tingyun application-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return applicationRetry(sdkgo.FailureRateLimit, "Tingyun rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return applicationRetry(sdkgo.FailureAvailability, "Tingyun application list is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyApplicationHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return applicationBranch(ListApplicationsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Tingyun response exceeded the configured limit")
	}
	if err != nil {
		return applicationRetry(sdkgo.FailureAvailability, "Tingyun response was interrupted", 0)
	}
	var wire []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if json.Unmarshal(body, &wire) != nil || wire == nil {
		return applicationBranch(ListApplicationsBranchInvalidResponse, sdkgo.FailureProtocol, "Tingyun returned invalid application metadata")
	}
	output := ListApplicationsOutput{Applications: make([]ApplicationMetadata, 0, len(wire))}
	for _, app := range wire {
		if app.ID <= 0 || app.Name == "" {
			return applicationBranch(ListApplicationsBranchInvalidResponse, sdkgo.FailureProtocol, "Tingyun returned incomplete application metadata")
		}
		output.Applications = append(output.Applications, ApplicationMetadata{ID: app.ID, Name: app.Name})
	}
	return sdkgo.NewQueryBranch(ListApplicationsBranchListed, output, nil, sdkgo.Receipt{})
}
func classifyApplicationHTTPStatus(status int) sdkgo.QueryAttempt[ListApplicationsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return applicationBranch(ListApplicationsBranchProviderRejected, kind, "Tingyun rejected the application-list request with HTTP "+strconv.Itoa(status))
}
func applicationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListApplicationsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tingyun", Operation: "listApplications", Message: message}
	return sdkgo.NewQueryBranch(branch, ListApplicationsOutput{}, &failure, sdkgo.Receipt{})
}
func applicationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListApplicationsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tingyun", Operation: "listApplications", Message: message}
	return sdkgo.NewQueryRetry[ListApplicationsOutput](failure, delay)
}
