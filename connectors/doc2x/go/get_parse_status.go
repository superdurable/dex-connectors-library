// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package doc2x

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetParseStatusInput names a PDF parsing task created by Doc2X preupload.
type GetParseStatusInput struct {
	// UID is the opaque task identifier returned by preupload.
	UID string `json:"uid"`
}

// GetParseStatusOutput contains only task state and completion percent.
type GetParseStatusOutput struct {
	// Status is processing, failed, or success.
	Status string `json:"status"`
	// Progress is a percentage between zero and one hundred.
	Progress int `json:"progress"`
}

// GetParseStatusOperation reads a Doc2X parsing task without exposing document contents.
type GetParseStatusOperation struct{ client *Client }

// Definition returns the generated parse-status contract.
func (GetParseStatusOperation) Definition() sdkgo.QueryDefinition { return GetParseStatusDefinition }

// Invoke reads the current task state and omits result pages, URLs, and failure detail.
func (operation GetParseStatusOperation) Invoke(call sdkgo.Call, input GetParseStatusInput) sdkgo.QueryAttempt[GetParseStatusOutput] {
	if input.UID == "" || len(input.UID) > 256 || strings.TrimSpace(input.UID) != input.UID || strings.ContainsAny(input.UID, "\r\n\x00") {
		return statusBranch(GetParseStatusBranchDefect, sdkgo.FailureValidation, "Doc2X task identifier is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return statusBranch(GetParseStatusBranchDefect, sdkgo.FailureAuthentication, "Doc2X API Key is unavailable or invalid")
	}
	endpoint, err := url.Parse(parseStatusEndpoint)
	if err != nil {
		return statusBranch(GetParseStatusBranchDefect, sdkgo.FailureValidation, "Doc2X status endpoint is invalid")
	}
	query := endpoint.Query()
	query.Set("uid", input.UID)
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return statusBranch(GetParseStatusBranchDefect, sdkgo.FailureValidation, "Doc2X status request is invalid")
	}
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return statusRetry(sdkgo.FailureAvailability, "Doc2X status request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return statusRetry(sdkgo.FailureRateLimit, "Doc2X rate limited status query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return statusRetry(sdkgo.FailureAvailability, "Doc2X status API is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyStatusHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return statusBranch(GetParseStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Doc2X status response exceeded configured limit")
	}
	if err != nil {
		return statusRetry(sdkgo.FailureAvailability, "Doc2X status response was interrupted", 0)
	}
	var wire struct {
		Code string `json:"code"`
		Data struct {
			Status   string `json:"status"`
			Progress *int   `json:"progress"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return statusBranch(GetParseStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Doc2X returned invalid status data")
	}
	if wire.Code != "success" {
		return statusBranch(GetParseStatusBranchProviderRejected, sdkgo.FailureProviderRejection, "Doc2X rejected status query")
	}
	if wire.Data.Status != "processing" && wire.Data.Status != "failed" && wire.Data.Status != "success" {
		return statusBranch(GetParseStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Doc2X returned unknown task state")
	}
	progress := 0
	if wire.Data.Progress != nil {
		progress = *wire.Data.Progress
	}
	if progress < 0 || progress > 100 {
		return statusBranch(GetParseStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Doc2X returned invalid progress")
	}
	return sdkgo.NewQueryBranch(GetParseStatusBranchFound, GetParseStatusOutput{Status: wire.Data.Status, Progress: progress}, nil, sdkgo.Receipt{})
}

func classifyStatusHTTPStatus(status int) sdkgo.QueryAttempt[GetParseStatusOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return statusBranch(GetParseStatusBranchProviderRejected, kind, "Doc2X rejected status query with HTTP "+strconv.Itoa(status))
}
func statusBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetParseStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "doc2x", Operation: "getParseStatus", Message: message}
	return sdkgo.NewQueryBranch(branch, GetParseStatusOutput{}, &failure, sdkgo.Receipt{})
}
func statusRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetParseStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "doc2x", Operation: "getParseStatus", Message: message}
	return sdkgo.NewQueryRetry[GetParseStatusOutput](failure, delay)
}
