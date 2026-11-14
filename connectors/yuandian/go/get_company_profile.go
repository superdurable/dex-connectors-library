// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package yuandian

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

// GetCompanyProfileInput identifies one company by its unified social credit code.
type GetCompanyProfileInput struct {
	// CreditCode is the 18-character uppercase unified social credit code.
	CreditCode string `json:"creditCode"`
}

// GetCompanyProfileOutput retains selected company registration facts only.
type GetCompanyProfileOutput struct {
	// CompanyID is the provider's company identifier.
	CompanyID string `json:"companyID"`
	// Name is the registered company name.
	Name string `json:"name"`
	// CreditCode is the unified social credit code.
	CreditCode string `json:"creditCode"`
	// RegistrationStatus is the registered operating status.
	RegistrationStatus string `json:"registrationStatus"`
	// Industry is the provider's industry classification.
	Industry string `json:"industry"`
	// CompanyType is the registered organization type.
	CompanyType string `json:"companyType"`
}

// GetCompanyProfileOperation reads one company without changing provider state.
type GetCompanyProfileOperation struct{ client *Client }

// Definition returns the generated company profile contract.
func (GetCompanyProfileOperation) Definition() sdkgo.QueryDefinition {
	return GetCompanyProfileDefinition
}

// Invoke reads one company profile and excludes provider personal information from the output.
func (operation GetCompanyProfileOperation) Invoke(call sdkgo.Call, input GetCompanyProfileInput) sdkgo.QueryAttempt[GetCompanyProfileOutput] {
	if len(input.CreditCode) != 18 || strings.Trim(input.CreditCode, "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ") != "" {
		return profileBranch(GetCompanyProfileBranchDefect, sdkgo.FailureValidation, "Yuandian requires one 18-character uppercase credit code")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return profileBranch(GetCompanyProfileBranchDefect, sdkgo.FailureAuthentication, "Yuandian API Key is unavailable or invalid")
	}
	query := url.Values{"tyshxydm": {input.CreditCode}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, companyProfileEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return profileBranch(GetCompanyProfileBranchDefect, sdkgo.FailureValidation, "Yuandian company request is invalid")
	}
	request.Header.Set("X-API-Key", credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return profileRetry(sdkgo.FailureAvailability, "Yuandian company request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return profileRetry(sdkgo.FailureRateLimit, "Yuandian rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return profileRetry(sdkgo.FailureAvailability, "Yuandian company service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyProfileHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return profileBranch(GetCompanyProfileBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Yuandian response exceeded the configured limit")
	}
	if err != nil {
		return profileRetry(sdkgo.FailureAvailability, "Yuandian response was interrupted", 0)
	}
	var wire struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
		Data    *struct {
			CompanyID          string `json:"企业ID"`
			Name               string `json:"企业名称"`
			CreditCode         string `json:"统一社会信用代码"`
			RegistrationStatus string `json:"经营状态"`
			Industry           string `json:"行业"`
			CompanyType        string `json:"企业类型"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Code == nil {
		return profileBranch(GetCompanyProfileBranchInvalidResponse, sdkgo.FailureProtocol, "Yuandian returned invalid company data")
	}
	if *wire.Code == 429 {
		return profileRetry(sdkgo.FailureRateLimit, "Yuandian rate limited the request", retryAfter)
	}
	if *wire.Code >= 500 {
		return profileRetry(sdkgo.FailureAvailability, "Yuandian company service is temporarily unavailable", retryAfter)
	}
	if *wire.Code != 200 {
		kind := sdkgo.FailureProviderRejection
		if *wire.Code == 401 {
			kind = sdkgo.FailureAuthentication
		}
		if *wire.Code == 403 {
			kind = sdkgo.FailureAuthorization
		}
		return profileBranch(GetCompanyProfileBranchProviderRejected, kind, "Yuandian rejected company request with code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data == nil {
		if strings.Contains(wire.Message, "未查询到") {
			return profileBranch(GetCompanyProfileBranchNoData, sdkgo.FailureNotFound, "Yuandian found no matching company")
		}
		return profileBranch(GetCompanyProfileBranchInvalidResponse, sdkgo.FailureProtocol, "Yuandian returned no company data")
	}
	data := wire.Data
	if data.CompanyID == "" || data.Name == "" || data.CreditCode != input.CreditCode || len(data.CompanyID) > 255 || len(data.Name) > 255 || len(data.RegistrationStatus) > 100 || len(data.Industry) > 255 || len(data.CompanyType) > 255 {
		return profileBranch(GetCompanyProfileBranchInvalidResponse, sdkgo.FailureProtocol, "Yuandian returned incomplete company profile")
	}
	output := GetCompanyProfileOutput{CompanyID: data.CompanyID, Name: data.Name, CreditCode: data.CreditCode, RegistrationStatus: data.RegistrationStatus, Industry: data.Industry, CompanyType: data.CompanyType}
	return sdkgo.NewQueryBranch(GetCompanyProfileBranchFound, output, nil, sdkgo.Receipt{})
}

func classifyProfileHTTPStatus(status int) sdkgo.QueryAttempt[GetCompanyProfileOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return profileBranch(GetCompanyProfileBranchProviderRejected, kind, "Yuandian rejected company request with HTTP "+strconv.Itoa(status))
}
func profileBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetCompanyProfileOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "yuandian", Operation: "getCompanyProfile", Message: message}
	return sdkgo.NewQueryBranch(branch, GetCompanyProfileOutput{}, &failure, sdkgo.Receipt{})
}
func profileRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetCompanyProfileOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "yuandian", Operation: "getCompanyProfile", Message: message}
	return sdkgo.NewQueryRetry[GetCompanyProfileOutput](failure, delay)
}
