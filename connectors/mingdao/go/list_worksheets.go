// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mingdao

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

// ListWorksheetsInput has no provider-specific query parameters.
type ListWorksheetsInput struct{}

// Worksheet identifies one application worksheet without exposing records.
type Worksheet struct {
	// ID is the stable worksheet ID.
	ID string `json:"id"`
	// Name is the worksheet display name.
	Name string `json:"name"`
}

// ListWorksheetsOutput contains the application's worksheet metadata.
type ListWorksheetsOutput struct {
	// Worksheets are the worksheets visible to the application's authorization key.
	Worksheets []Worksheet `json:"worksheets"`
}

// ListWorksheetsOperation reads worksheet metadata without mutating provider state.
type ListWorksheetsOperation struct{ client *Client }

// Definition returns the generated worksheet-list contract.
func (ListWorksheetsOperation) Definition() sdkgo.QueryDefinition { return ListWorksheetsDefinition }

// Invoke reads the worksheet list and omits row data and remarks.
func (operation ListWorksheetsOperation) Invoke(call sdkgo.Call, _ ListWorksheetsInput) sdkgo.QueryAttempt[ListWorksheetsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidSecret(credentials.AppKey.Reveal()) || !isValidSecret(credentials.Sign.Reveal()) {
		return worksheetBranch(ListWorksheetsBranchDefect, sdkgo.FailureAuthentication, "Mingdao app credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, worksheetsEndpoint, bytes.NewBufferString(`{"responseFormat":"json"}`))
	if err != nil {
		return worksheetBranch(ListWorksheetsBranchDefect, sdkgo.FailureValidation, "Mingdao worksheet-list request is invalid")
	}
	request.Header.Set("HAP-Appkey", credentials.AppKey.Reveal())
	request.Header.Set("HAP-Sign", credentials.Sign.Reveal())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return worksheetRetry(sdkgo.FailureAvailability, "Mingdao worksheet-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return worksheetRetry(sdkgo.FailureRateLimit, "Mingdao rate limited the worksheet-list request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return worksheetRetry(sdkgo.FailureAvailability, "Mingdao worksheet list is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyWorksheetHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return worksheetBranch(ListWorksheetsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Mingdao response exceeded the configured limit")
	}
	if err != nil {
		return worksheetRetry(sdkgo.FailureAvailability, "Mingdao worksheet-list response was interrupted", 0)
	}
	var wire struct {
		Success   *bool `json:"success"`
		ErrorCode *int  `json:"error_code"`
		Data      []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Success == nil {
		return worksheetBranch(ListWorksheetsBranchInvalidResponse, sdkgo.FailureProtocol, "Mingdao returned invalid worksheet metadata")
	}
	if !*wire.Success {
		if wire.ErrorCode == nil {
			return worksheetBranch(ListWorksheetsBranchProviderRejected, sdkgo.FailureProviderRejection, "Mingdao rejected the worksheet-list request")
		}
		if *wire.ErrorCode == 51 || *wire.ErrorCode == 90000 {
			return worksheetRetry(sdkgo.FailureRateLimit, "Mingdao rate limited the worksheet-list request", 0)
		}
		if *wire.ErrorCode == 99999 {
			return worksheetRetry(sdkgo.FailureAvailability, "Mingdao worksheet list is temporarily unavailable", 0)
		}
		return worksheetBranch(ListWorksheetsBranchProviderRejected, sdkgo.FailureProviderRejection, "Mingdao rejected the worksheet-list request with code "+strconv.Itoa(*wire.ErrorCode))
	}
	if (wire.ErrorCode != nil && *wire.ErrorCode != 1) || wire.Data == nil {
		return worksheetBranch(ListWorksheetsBranchInvalidResponse, sdkgo.FailureProtocol, "Mingdao returned invalid worksheet metadata")
	}
	output := ListWorksheetsOutput{Worksheets: make([]Worksheet, 0, len(wire.Data))}
	for _, worksheet := range wire.Data {
		if worksheet.ID == "" || worksheet.Name == "" {
			return worksheetBranch(ListWorksheetsBranchInvalidResponse, sdkgo.FailureProtocol, "Mingdao returned incomplete worksheet metadata")
		}
		output.Worksheets = append(output.Worksheets, Worksheet{ID: worksheet.ID, Name: worksheet.Name})
	}
	return sdkgo.NewQueryBranch(ListWorksheetsBranchListed, output, nil, sdkgo.Receipt{})
}
func classifyWorksheetHTTPStatus(status int) sdkgo.QueryAttempt[ListWorksheetsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return worksheetBranch(ListWorksheetsBranchProviderRejected, kind, "Mingdao rejected the worksheet-list request with HTTP "+strconv.Itoa(status))
}
func worksheetBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListWorksheetsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "mingdao", Operation: "listWorksheets", Message: message}
	return sdkgo.NewQueryBranch(branch, ListWorksheetsOutput{}, &failure, sdkgo.Receipt{})
}
func worksheetRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListWorksheetsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "mingdao", Operation: "listWorksheets", Message: message}
	return sdkgo.NewQueryRetry[ListWorksheetsOutput](failure, delay)
}
