// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qingflow

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListApplicationsInput requires no selection because the accessToken determines the workspace.
type ListApplicationsInput struct{}

// ApplicationMetadata identifies an application without its records or form contents.
type ApplicationMetadata struct {
	// AppKey is the application's opaque provider key.
	AppKey string `json:"appKey"`
	// AppName is its display name.
	AppName string `json:"appName"`
}

// ListApplicationsOutput contains only application keys and names.
type ListApplicationsOutput struct {
	// Applications are the applications in the token's workspace.
	Applications []ApplicationMetadata `json:"applications"`
}

// ListApplicationsOperation reads application metadata without modifying provider state.
type ListApplicationsOperation struct{ client *Client }

// Definition returns the generated application-list contract.
func (ListApplicationsOperation) Definition() sdkgo.QueryDefinition {
	return ListApplicationsDefinition
}

// Invoke lists applications and discards all other response fields.
func (operation ListApplicationsOperation) Invoke(call sdkgo.Call, _ ListApplicationsInput) sdkgo.QueryAttempt[ListApplicationsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAccessToken(credentials.AccessToken.Reveal()) {
		return applicationsBranch(ListApplicationsBranchDefect, sdkgo.FailureAuthentication, "Qingflow accessToken is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, applicationsEndpoint, nil)
	if err != nil {
		return applicationsBranch(ListApplicationsBranchDefect, sdkgo.FailureValidation, "Qingflow application-list request is invalid")
	}
	request.Header.Set("accessToken", credentials.AccessToken.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return applicationsRetry(sdkgo.FailureAvailability, "Qingflow application-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return applicationsRetry(sdkgo.FailureRateLimit, "Qingflow rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return applicationsRetry(sdkgo.FailureAvailability, "Qingflow application list is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyApplicationsHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return applicationsBranch(ListApplicationsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Qingflow response exceeded the configured limit")
	}
	if err != nil {
		return applicationsRetry(sdkgo.FailureAvailability, "Qingflow response was interrupted", 0)
	}
	var wire struct {
		ErrCode *int `json:"errCode"`
		Result  *struct {
			AppList []struct {
				AppKey  string `json:"appKey"`
				AppName string `json:"appName"`
			} `json:"appList"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.ErrCode == nil {
		return applicationsBranch(ListApplicationsBranchInvalidResponse, sdkgo.FailureProtocol, "Qingflow returned invalid application metadata")
	}
	if *wire.ErrCode != 0 {
		return applicationsBranch(ListApplicationsBranchProviderRejected, sdkgo.FailureProviderRejection, "Qingflow rejected the application-list request with code "+strconv.Itoa(*wire.ErrCode))
	}
	if wire.Result == nil || wire.Result.AppList == nil {
		return applicationsBranch(ListApplicationsBranchInvalidResponse, sdkgo.FailureProtocol, "Qingflow returned incomplete application metadata")
	}
	output := ListApplicationsOutput{Applications: make([]ApplicationMetadata, 0, len(wire.Result.AppList))}
	for _, application := range wire.Result.AppList {
		if application.AppKey == "" || application.AppName == "" {
			return applicationsBranch(ListApplicationsBranchInvalidResponse, sdkgo.FailureProtocol, "Qingflow returned an incomplete application")
		}
		output.Applications = append(output.Applications, ApplicationMetadata{AppKey: application.AppKey, AppName: application.AppName})
	}
	return sdkgo.NewQueryBranch(ListApplicationsBranchListed, output, nil, sdkgo.Receipt{})
}

func classifyApplicationsHTTPStatus(status int) sdkgo.QueryAttempt[ListApplicationsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return applicationsBranch(ListApplicationsBranchProviderRejected, kind, "Qingflow rejected the application-list request with HTTP "+strconv.Itoa(status))
}
func applicationsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListApplicationsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "qingflow", Operation: "listApplications", Message: message}
	return sdkgo.NewQueryBranch(branch, ListApplicationsOutput{}, &failure, sdkgo.Receipt{})
}
func applicationsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListApplicationsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "qingflow", Operation: "listApplications", Message: message}
	return sdkgo.NewQueryRetry[ListApplicationsOutput](failure, delay)
}
