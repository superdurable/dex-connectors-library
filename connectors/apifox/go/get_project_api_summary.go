// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package apifox

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// GetProjectAPISummaryInput identifies one Apifox project by positive decimal ID.
type GetProjectAPISummaryInput struct {
	// ProjectID is the decimal ID shown in Apifox Project Settings.
	ProjectID string `json:"projectID"`
}

// GetProjectAPISummaryOutput excludes the exported API document and endpoint details.
type GetProjectAPISummaryOutput struct {
	// Title is the exported OpenAPI info title.
	Title string `json:"title"`
	// Version is the exported OpenAPI info version.
	Version string `json:"version"`
	// EndpointCount counts HTTP methods across the exported paths.
	EndpointCount int `json:"endpointCount"`
}

// GetProjectAPISummaryOperation reads an accessible project without changing it.
type GetProjectAPISummaryOperation struct{ client *Client }

// Definition returns the generated operation contract.
func (GetProjectAPISummaryOperation) Definition() sdkgo.QueryDefinition {
	return GetProjectAPISummaryDefinition
}

// Invoke exports OpenAPI 3.1 JSON and returns only its bounded summary.
func (operation GetProjectAPISummaryOperation) Invoke(call sdkgo.Call, input GetProjectAPISummaryInput) sdkgo.QueryAttempt[GetProjectAPISummaryOutput] {
	if !isValidProjectID(input.ProjectID) {
		return projectBranch(GetProjectAPISummaryBranchDefect, sdkgo.FailureValidation, "Apifox project ID must be a positive decimal string")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAccessToken(credentials.AccessToken.Reveal()) {
		return projectBranch(GetProjectAPISummaryBranchDefect, sdkgo.FailureAuthentication, "Apifox access token is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, "https://api.apifox.com/v1/projects/"+input.ProjectID+"/export-openapi", bytes.NewBufferString(`{"scope":{"type":"ALL"},"oasVersion":"3.1","exportFormat":"JSON"}`))
	if err != nil {
		return projectBranch(GetProjectAPISummaryBranchDefect, sdkgo.FailureValidation, "Apifox project request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	request.Header.Set("X-Apifox-Api-Version", "2024-03-28")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return projectRetry(sdkgo.FailureAvailability, "Apifox project request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return projectRetry(sdkgo.FailureRateLimit, "Apifox rate limited the project request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return projectRetry(sdkgo.FailureAvailability, "Apifox project export is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyProjectHTTPStatus(response.StatusCode)
	}
	payload, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return projectBranch(GetProjectAPISummaryBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Apifox export exceeded the configured limit")
	}
	if err != nil {
		return projectRetry(sdkgo.FailureAvailability, "Apifox export was interrupted", 0)
	}
	var wire struct {
		OpenAPI string `json:"openapi"`
		Info    struct {
			Title   string `json:"title"`
			Version string `json:"version"`
		} `json:"info"`
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if json.Unmarshal(payload, &wire) != nil || !strings.HasPrefix(wire.OpenAPI, "3.1") || wire.Paths == nil || wire.Info.Title == "" || wire.Info.Version == "" || len(wire.Info.Title) > 256 || len(wire.Info.Version) > 64 || strings.ContainsAny(wire.Info.Title+wire.Info.Version, "\r\n\x00") || strings.Contains(wire.Info.Title+wire.Info.Version, credentials.AccessToken.Reveal()) {
		return projectBranch(GetProjectAPISummaryBranchInvalidResponse, sdkgo.FailureProtocol, "Apifox returned invalid OpenAPI data")
	}
	count := 0
	for _, methods := range wire.Paths {
		for method := range methods {
			switch strings.ToLower(method) {
			case "get", "post", "put", "patch", "delete", "head", "options", "trace":
				count++
			}
		}
	}
	return sdkgo.NewQueryBranch(GetProjectAPISummaryBranchFound, GetProjectAPISummaryOutput{Title: wire.Info.Title, Version: wire.Info.Version, EndpointCount: count}, nil, sdkgo.Receipt{})
}
func isValidProjectID(projectID string) bool {
	if len(projectID) == 0 || len(projectID) > 20 || projectID[0] == '0' {
		return false
	}
	for _, digit := range projectID {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
func classifyProjectHTTPStatus(status int) sdkgo.QueryAttempt[GetProjectAPISummaryOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return projectBranch(GetProjectAPISummaryBranchProviderRejected, kind, "Apifox rejected project export with HTTP "+strconv.Itoa(status))
}
func projectBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetProjectAPISummaryOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "apifox", Operation: "getProjectAPISummary", Message: message}
	return sdkgo.NewQueryBranch(branch, GetProjectAPISummaryOutput{}, &failure, sdkgo.Receipt{})
}
func projectRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetProjectAPISummaryOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "apifox", Operation: "getProjectAPISummary", Message: message}
	return sdkgo.NewQueryRetry[GetProjectAPISummaryOutput](failure, delay)
}
