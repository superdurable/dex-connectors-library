// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oraclenetsuite

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var vendorIDPattern = regexp.MustCompile(`^[1-9][0-9]*$`)

// GetVendorInput identifies a vendor by its internal NetSuite ID.
type GetVendorInput struct {
	// VendorID is the non-secret numeric internal ID shown on the vendor record.
	VendorID string `json:"vendorId"`
}

// VendorSummary contains the vendor's internal and entity IDs.
type VendorSummary struct {
	// ID is the vendor's internal ID.
	ID string `json:"id"`
	// EntityID is the vendor ID displayed by NetSuite.
	EntityID string `json:"entityId"`
}

// GetVendorOperation reads a vendor without mutating the provider.
type GetVendorOperation struct{ client *Client }

// GetVendor binds the Query to this client.
func (client *Client) GetVendor() GetVendorOperation { return GetVendorOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetVendorOperation) Definition() sdkgo.QueryDefinition { return GetVendorDefinition }

// Invoke classifies token, transport, HTTP, and vendor response outcomes.
func (operation GetVendorOperation) Invoke(call sdkgo.Call, input GetVendorInput) sdkgo.QueryAttempt[VendorSummary] {
	if !vendorIDPattern.MatchString(input.VendorID) {
		return vendorBranch(GetVendorBranchDefect, sdkgo.FailureValidation, "vendor internal ID is invalid")
	}
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return vendorBranch(GetVendorBranchDefect, sdkgo.FailureAuthentication, "Oracle NetSuite connection needs authorization")
		}
		return vendorRetry(sdkgo.FailureTransport, "Oracle NetSuite credential refresh failed", 0)
	}
	accessToken := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return vendorBranch(GetVendorBranchDefect, sdkgo.FailureAuthentication, "Oracle NetSuite access token is unavailable")
	}
	endpoint := operation.client.baseURL + "/services/rest/record/v1/vendor/" + url.PathEscape(input.VendorID)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return vendorBranch(GetVendorBranchDefect, sdkgo.FailureValidation, "vendor request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return vendorRetry(sdkgo.FailureTransport, "Oracle NetSuite vendor request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return vendorBranch(GetVendorBranchDefect, sdkgo.FailureAuthentication, "Oracle NetSuite response contained a credential")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return vendorRetry(sdkgo.FailureRateLimit, "Oracle NetSuite rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return vendorRetry(sdkgo.FailureAvailability, "Oracle NetSuite is temporarily unavailable", retryAfter)
	}
	if response.StatusCode == http.StatusNotFound {
		return vendorBranch(GetVendorBranchNotFound, sdkgo.FailureNotFound, "Oracle NetSuite vendor was not found")
	}
	if response.StatusCode != http.StatusOK {
		return vendorBranch(GetVendorBranchProviderRejected, sdkgo.FailureProviderRejection, "Oracle NetSuite rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
		return vendorBranch(GetVendorBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Oracle NetSuite vendor response exceeded the configured limit")
	}
	if readErr != nil {
		return vendorRetry(sdkgo.FailureTransport, "Oracle NetSuite vendor response was interrupted", 0)
	}
	vendor, err := decodeVendor(body, input.VendorID)
	if err != nil {
		return vendorBranch(GetVendorBranchInvalidResponse, sdkgo.FailureProtocol, "Oracle NetSuite returned invalid vendor JSON")
	}
	return sdkgo.NewQueryBranch(GetVendorBranchFound, vendor, nil, sdkgo.Receipt{})
}

func decodeVendor(body []byte, requestedID string) (VendorSummary, error) {
	var vendor VendorSummary
	if err := json.Unmarshal(body, &vendor); err != nil {
		return VendorSummary{}, err
	}
	if vendor.ID != requestedID || vendor.EntityID == "" {
		return VendorSummary{}, errors.New("vendor result is incomplete or has a different internal ID")
	}
	return vendor, nil
}

func vendorFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "oracle-netsuite", Operation: "getVendor", Message: message}
}

func vendorBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[VendorSummary] {
	failure := vendorFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, VendorSummary{}, &failure, sdkgo.Receipt{})
}

func vendorRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[VendorSummary] {
	return sdkgo.NewQueryRetry[VendorSummary](vendorFailure(kind, message), delay)
}
