// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package esign

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"strconv"
	"time"
)

// GetSignFlowStatusInput identifies an existing signing flow.
type GetSignFlowStatusInput struct {
	// SignFlowID is the opaque flow ID returned by an existing eSign signing workflow.
	SignFlowID string `json:"signFlowId"`
}

// GetSignFlowStatusOutput contains only the signing flow ID and numeric status.
type GetSignFlowStatusOutput struct {
	// SignFlowID is the requested signing flow identifier.
	SignFlowID string `json:"signFlowId"`
	// SignFlowStatus is eSign's numeric signing-flow state.
	SignFlowStatus int `json:"signFlowStatus"`
}

// GetSignFlowStatusOperation reads signing status without modifying provider state.
type GetSignFlowStatusOperation struct{ client *Client }

// Definition returns the generated signing-status contract.
func (GetSignFlowStatusOperation) Definition() sdkgo.QueryDefinition {
	return GetSignFlowStatusDefinition
}

// Invoke signs a GET request and discards document and signer fields.
func (operation GetSignFlowStatusOperation) Invoke(call sdkgo.Call, input GetSignFlowStatusInput) sdkgo.QueryAttempt[GetSignFlowStatusOutput] {
	if !isValidSignFlowID(input.SignFlowID) {
		return statusBranch(GetSignFlowStatusBranchDefect, sdkgo.FailureValidation, "eSign signFlowId is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppID) || !isValidCredential(credentials.AppSecret.Reveal()) {
		return statusBranch(GetSignFlowStatusBranchDefect, sdkgo.FailureAuthentication, "eSign app credentials are unavailable or invalid")
	}
	path := "/v3/sign-flow/" + input.SignFlowID + "/detail"
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, signFlowEndpoint+path, nil)
	if err != nil {
		return statusBranch(GetSignFlowStatusBranchDefect, sdkgo.FailureValidation, "eSign status request is invalid")
	}
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Content-Type", "application/json; charset=UTF-8")
	request.Header.Set("X-Tsign-Open-App-Id", credentials.AppID)
	request.Header.Set("X-Tsign-Open-Auth-Mode", "Signature")
	request.Header.Set("X-Tsign-Open-Ca-Timestamp", strconv.FormatInt(operation.client.clock().UnixMilli(), 10))
	request.Header.Set("X-Tsign-Open-Ca-Signature", signReadRequest(credentials.AppSecret.Reveal(), path))
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return statusRetry(sdkgo.FailureAvailability, "eSign status request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return statusRetry(sdkgo.FailureRateLimit, "eSign rate limited the status request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return statusRetry(sdkgo.FailureAvailability, "eSign status is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyStatusHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return statusBranch(GetSignFlowStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "eSign response exceeded the configured limit")
	}
	if err != nil {
		return statusRetry(sdkgo.FailureAvailability, "eSign status response was interrupted", 0)
	}
	var wire struct {
		Code *int `json:"code"`
		Data *struct {
			SignFlowStatus *int `json:"signFlowStatus"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return statusBranch(GetSignFlowStatusBranchInvalidResponse, sdkgo.FailureProtocol, "eSign returned invalid status data")
	}
	if *wire.Code != 0 {
		return statusBranch(GetSignFlowStatusBranchProviderRejected, sdkgo.FailureProviderRejection, "eSign rejected the status request with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data == nil || wire.Data.SignFlowStatus == nil || *wire.Data.SignFlowStatus < 0 {
		return statusBranch(GetSignFlowStatusBranchInvalidResponse, sdkgo.FailureProtocol, "eSign returned incomplete status data")
	}
	return sdkgo.NewQueryBranch(GetSignFlowStatusBranchFound, GetSignFlowStatusOutput{SignFlowID: input.SignFlowID, SignFlowStatus: *wire.Data.SignFlowStatus}, nil, sdkgo.Receipt{})
}
func signReadRequest(secret, path string) string {
	// Empty Content-MD5 and Date fields retain their separators in eSign's canonical string.
	canonical := "GET\n*/*\n\napplication/json; charset=UTF-8\n\n" + path
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
func classifyStatusHTTPStatus(status int) sdkgo.QueryAttempt[GetSignFlowStatusOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return statusBranch(GetSignFlowStatusBranchProviderRejected, kind, "eSign rejected the status request with HTTP "+strconv.Itoa(status))
}
func statusBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetSignFlowStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "esign", Operation: "getSignFlowStatus", Message: message}
	return sdkgo.NewQueryBranch(branch, GetSignFlowStatusOutput{}, &failure, sdkgo.Receipt{})
}
func statusRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetSignFlowStatusOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "esign", Operation: "getSignFlowStatus", Message: message}
	return sdkgo.NewQueryRetry[GetSignFlowStatusOutput](failure, delay)
}
