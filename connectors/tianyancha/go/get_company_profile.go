// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tianyancha

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

// GetCompanyProfileInput identifies one company by name, ID, registration number, or credit code.
type GetCompanyProfileInput struct {
	// Keyword is one company name, ID, registration number, or unified social credit code.
	Keyword string `json:"keyword"`
}

// GetCompanyProfileOutput includes selected public business registration facts only.
type GetCompanyProfileOutput struct {
	// CompanyID is Tianyancha's company identifier.
	CompanyID int64 `json:"companyID"`
	// Name is the registered company name.
	Name string `json:"name"`
	// RegistrationStatus is the provider's company status.
	RegistrationStatus string `json:"registrationStatus"`
	// CreditCode is the unified social credit code.
	CreditCode string `json:"creditCode"`
	// Industry is the provider's industry classification.
	Industry string `json:"industry"`
	// RegisteredCapital is the textual capital amount and currency.
	RegisteredCapital string `json:"registeredCapital"`
	// CompanyType is the registered organization type.
	CompanyType string `json:"companyType"`
}

// GetCompanyProfileOperation reads selected business facts without changing provider state.
type GetCompanyProfileOperation struct{ client *Client }

// Definition returns the generated company profile contract.
func (GetCompanyProfileOperation) Definition() sdkgo.QueryDefinition {
	return GetCompanyProfileDefinition
}

// Invoke reads one company profile; the provider may charge for each attempt.
func (operation GetCompanyProfileOperation) Invoke(call sdkgo.Call, input GetCompanyProfileInput) sdkgo.QueryAttempt[GetCompanyProfileOutput] {
	keyword := strings.TrimSpace(input.Keyword)
	if keyword == "" || keyword != input.Keyword || len(keyword) > 255 || strings.ContainsAny(keyword, "\r\n\x00") {
		return profileBranch(GetCompanyProfileBranchDefect, sdkgo.FailureValidation, "Tianyancha requires one valid company keyword")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return profileBranch(GetCompanyProfileBranchDefect, sdkgo.FailureAuthentication, "Tianyancha API token is unavailable or invalid")
	}
	query := url.Values{"keyword": {keyword}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, companyProfileEndpoint+"?"+query.Encode(), nil)
	if err != nil {
		return profileBranch(GetCompanyProfileBranchDefect, sdkgo.FailureValidation, "Tianyancha company request is invalid")
	}
	request.Header.Set("Authorization", credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return profileRetry(sdkgo.FailureAvailability, "Tianyancha company request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return profileRetry(sdkgo.FailureRateLimit, "Tianyancha rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return profileRetry(sdkgo.FailureAvailability, "Tianyancha company service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifyProfileHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return profileBranch(GetCompanyProfileBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Tianyancha response exceeded the configured limit")
	}
	if err != nil {
		return profileRetry(sdkgo.FailureAvailability, "Tianyancha response was interrupted", 0)
	}
	var wire struct {
		ErrorCode *int `json:"error_code"`
		Result    *struct {
			CompanyID          int64  `json:"id"`
			Name               string `json:"name"`
			RegistrationStatus string `json:"regStatus"`
			CreditCode         string `json:"creditCode"`
			Industry           string `json:"industry"`
			RegisteredCapital  string `json:"regCapital"`
			CompanyType        string `json:"companyOrgType"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.ErrorCode == nil {
		return profileBranch(GetCompanyProfileBranchInvalidResponse, sdkgo.FailureProtocol, "Tianyancha returned invalid company data")
	}
	switch *wire.ErrorCode {
	case 0:
		if wire.Result == nil || wire.Result.CompanyID <= 0 || wire.Result.Name == "" || len(wire.Result.Name) > 255 || len(wire.Result.RegistrationStatus) > 100 || len(wire.Result.CreditCode) > 100 || len(wire.Result.Industry) > 255 || len(wire.Result.RegisteredCapital) > 100 || len(wire.Result.CompanyType) > 255 {
			return profileBranch(GetCompanyProfileBranchInvalidResponse, sdkgo.FailureProtocol, "Tianyancha returned invalid company profile")
		}
		result := GetCompanyProfileOutput{CompanyID: wire.Result.CompanyID, Name: wire.Result.Name, RegistrationStatus: wire.Result.RegistrationStatus, CreditCode: wire.Result.CreditCode, Industry: wire.Result.Industry, RegisteredCapital: wire.Result.RegisteredCapital, CompanyType: wire.Result.CompanyType}
		return sdkgo.NewQueryBranch(GetCompanyProfileBranchFound, result, nil, sdkgo.Receipt{})
	case 300000:
		return profileBranch(GetCompanyProfileBranchNoData, sdkgo.FailureNotFound, "Tianyancha found no matching company")
	case 300004:
		return profileRetry(sdkgo.FailureRateLimit, "Tianyancha frequency limit reached", retryAfter)
	case 300001, 300012:
		return profileRetry(sdkgo.FailureAvailability, "Tianyancha company service is temporarily unavailable", retryAfter)
	default:
		kind := sdkgo.FailureProviderRejection
		if *wire.ErrorCode == 300002 || *wire.ErrorCode == 300003 || *wire.ErrorCode == 300009 {
			kind = sdkgo.FailureAuthentication
		}
		if *wire.ErrorCode == 300005 || *wire.ErrorCode == 300011 {
			kind = sdkgo.FailureAuthorization
		}
		return profileBranch(GetCompanyProfileBranchProviderRejected, kind, "Tianyancha rejected company request with code "+strconv.Itoa(*wire.ErrorCode))
	}
}

func classifyProfileHTTPStatus(status int) sdkgo.QueryAttempt[GetCompanyProfileOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return profileBranch(GetCompanyProfileBranchProviderRejected, kind, "Tianyancha rejected company request with HTTP "+strconv.Itoa(status))
}

func profileBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetCompanyProfileOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tianyancha", Operation: "getCompanyProfile", Message: message}
	return sdkgo.NewQueryBranch(branch, GetCompanyProfileOutput{}, &failure, sdkgo.Receipt{})
}

func profileRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetCompanyProfileOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "tianyancha", Operation: "getCompanyProfile", Message: message}
	return sdkgo.NewQueryRetry[GetCompanyProfileOutput](failure, delay)
}
