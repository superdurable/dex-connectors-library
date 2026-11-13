// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package suxin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const authPath = "/api/v1/auth/token"
const organizationsPath = "/api/v1/org/list"

// ListOrganizationsInput selects the API user's accessible organizations without additional user input.
type ListOrganizationsInput struct{}

// OrganizationSummary excludes registration numbers and personal details.
type OrganizationSummary struct {
	// ID is the provider's organization ID.
	ID string `json:"id"`
	// Name is the organization's name.
	Name string `json:"name"`
	// EntityType distinguishes a company from a self-employed entity.
	EntityType string `json:"entityType"`
	// CertifyStatus is the provider's certification status.
	CertifyStatus string `json:"certifyStatus"`
}

// ListOrganizationsOutput contains the unpaginated organization list returned by Suxin.
type ListOrganizationsOutput struct {
	Organizations []OrganizationSummary `json:"organizations"`
}

// ListOrganizationsOperation reads organizations for the API key's default user.
type ListOrganizationsOperation struct{ client *Client }

// Definition returns the generated organization-list Query contract.
func (ListOrganizationsOperation) Definition() sdkgo.QueryDefinition {
	return ListOrganizationsDefinition
}

// Invoke obtains an ephemeral token and requests organizations for the provider-derived user ID.
func (operation ListOrganizationsOperation) Invoke(call sdkgo.Call, input ListOrganizationsInput) sdkgo.QueryAttempt[ListOrganizationsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppID.Reveal()) || !isValidCredential(credentials.APIKey.Reveal()) {
		return organizationBranch(ListOrganizationsBranchDefect, sdkgo.FailureAuthentication, "Suxin credentials are unavailable or invalid")
	}
	authBody, _ := json.Marshal(struct {
		AppID  string `json:"appId"`
		APIKey string `json:"apiKey"`
	}{credentials.AppID.Reveal(), credentials.APIKey.Reveal()})
	authRequest, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+authPath, strings.NewReader(string(authBody)))
	if err != nil {
		return organizationBranch(ListOrganizationsBranchDefect, sdkgo.FailureValidation, "authorization request is invalid")
	}
	authRequest.Header.Set("Content-Type", "application/json")
	authRequest.Header.Set("Accept", "application/json")
	authResponse, err := operation.client.httpClient.Do(authRequest)
	if err != nil {
		return organizationRetry(sdkgo.FailureAvailability, "Suxin authorization transport failed", 0)
	}
	authBytes, attempt := operation.readResponse(authResponse, "authorization")
	if attempt != nil {
		return *attempt
	}
	var authWire struct {
		Success *bool           `json:"success"`
		Code    json.RawMessage `json:"code"`
		Data    *struct {
			AccessToken string `json:"accessToken"`
			UserID      string `json:"userId"`
		} `json:"data"`
	}
	if !decodeOne(authBytes, &authWire) {
		return organizationBranch(ListOrganizationsBranchInvalidResponse, sdkgo.FailureProtocol, "Suxin returned invalid authorization JSON")
	}
	if authWire.Success == nil {
		return organizationBranch(ListOrganizationsBranchInvalidResponse, sdkgo.FailureProtocol, "Suxin authorization success is missing")
	}
	if !*authWire.Success {
		return organizationProviderFailure(authWire.Code, "authorization")
	}
	if authWire.Data == nil || !isValidCredential(authWire.Data.AccessToken) || !isValidIdentifier(authWire.Data.UserID) {
		return organizationBranch(ListOrganizationsBranchInvalidResponse, sdkgo.FailureProtocol, "Suxin returned invalid authorization data")
	}
	values := url.Values{}
	values.Set("userId", authWire.Data.UserID)
	organizationRequest, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+organizationsPath+"?"+values.Encode(), nil)
	if err != nil {
		return organizationBranch(ListOrganizationsBranchDefect, sdkgo.FailureValidation, "organization request is invalid")
	}
	organizationRequest.Header.Set("Accept", "application/json")
	organizationRequest.Header.Set("Authorization", "Bearer "+authWire.Data.AccessToken)
	response, err := operation.client.httpClient.Do(organizationRequest)
	if err != nil {
		return organizationRetry(sdkgo.FailureAvailability, "Suxin organization transport failed", 0)
	}
	body, attempt := operation.readResponse(response, "organization")
	if attempt != nil {
		return *attempt
	}
	var wire struct {
		Success *bool           `json:"success"`
		Code    json.RawMessage `json:"code"`
		Data    []struct {
			OrgID         string `json:"orgId"`
			Name          string `json:"name"`
			EntityType    string `json:"entityType"`
			CertifyStatus string `json:"certifyStatus"`
		} `json:"data"`
	}
	if !decodeOne(body, &wire) || wire.Success == nil {
		return organizationBranch(ListOrganizationsBranchInvalidResponse, sdkgo.FailureProtocol, "Suxin returned invalid organization JSON")
	}
	if !*wire.Success {
		return organizationProviderFailure(wire.Code, "organization")
	}
	if wire.Data == nil {
		return organizationBranch(ListOrganizationsBranchInvalidResponse, sdkgo.FailureProtocol, "Suxin organization list is missing")
	}
	organizations := make([]OrganizationSummary, 0, len(wire.Data))
	for _, item := range wire.Data {
		if !isValidIdentifier(item.OrgID) || item.Name == "" || len(item.Name) > 512 || (item.EntityType != "COMPANY" && item.EntityType != "SELF_EMPLOYED") || (item.CertifyStatus != "VERIFIED" && item.CertifyStatus != "UNVERIFIED") {
			return organizationBranch(ListOrganizationsBranchInvalidResponse, sdkgo.FailureProtocol, "Suxin returned invalid organization metadata")
		}
		organizations = append(organizations, OrganizationSummary{ID: item.OrgID, Name: item.Name, EntityType: item.EntityType, CertifyStatus: item.CertifyStatus})
	}
	return sdkgo.NewQueryBranch(ListOrganizationsBranchListed, ListOrganizationsOutput{Organizations: organizations}, nil, sdkgo.Receipt{})
}
func (operation ListOrganizationsOperation) readResponse(response *http.Response, stage string) ([]byte, *sdkgo.QueryAttempt[ListOrganizationsOutput]) {
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		attempt := organizationRetry(sdkgo.FailureRateLimit, "Suxin "+stage+" request rate limited", retryAfter)
		return nil, &attempt
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		attempt := organizationRetry(sdkgo.FailureAvailability, "Suxin "+stage+" temporarily unavailable", retryAfter)
		return nil, &attempt
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		attempt := organizationBranch(ListOrganizationsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Suxin "+stage+" response exceeded the configured limit")
		return nil, &attempt
	}
	if err != nil {
		attempt := organizationRetry(sdkgo.FailureAvailability, "Suxin "+stage+" response was interrupted", 0)
		return nil, &attempt
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == 401 || response.StatusCode == 403 {
			kind = sdkgo.FailureAuthentication
		}
		attempt := organizationBranch(ListOrganizationsBranchProviderRejected, kind, "Suxin rejected "+stage+" with HTTP "+strconv.Itoa(response.StatusCode))
		return nil, &attempt
	}
	return body, nil
}
func organizationProviderFailure(rawCode json.RawMessage, stage string) sdkgo.QueryAttempt[ListOrganizationsOutput] {
	code := strings.Trim(string(rawCode), `"`)
	if code == "5000" {
		return organizationRetry(sdkgo.FailureAvailability, "Suxin "+stage+" temporarily unavailable", 0)
	}
	kind := sdkgo.FailureProviderRejection
	if code == "4010" {
		kind = sdkgo.FailureAuthentication
	}
	return organizationBranch(ListOrganizationsBranchProviderRejected, kind, "Suxin rejected "+stage+" with code "+code)
}
func decodeOne(body []byte, target any) bool {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if decoder.Decode(target) != nil {
		return false
	}
	var extra any
	return decoder.Decode(&extra) == io.EOF
}
func isValidCredential(value string) bool {
	if value == "" || len(value) > 2048 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func isValidIdentifier(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
func organizationBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListOrganizationsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "suxin", Operation: "listOrganizations", Message: message}
	return sdkgo.NewQueryBranch(branch, ListOrganizationsOutput{}, &failure, sdkgo.Receipt{})
}
func organizationRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListOrganizationsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "suxin", Operation: "listOrganizations", Message: message}
	return sdkgo.NewQueryRetry[ListOrganizationsOutput](failure, delay)
}
