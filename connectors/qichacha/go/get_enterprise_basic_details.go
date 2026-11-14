// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qichacha

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetEnterpriseBasicDetailsInput identifies one mainland enterprise by company name or unified social credit code.
type GetEnterpriseBasicDetailsInput struct {
	// Keyword is the exact enterprise name or 18-character unified social credit code.
	Keyword string `json:"keyword"`
}

// GetEnterpriseBasicDetailsOutput contains only non-personal enterprise registration fields.
type GetEnterpriseBasicDetailsOutput struct {
	// Name is the registered enterprise name.
	Name string `json:"name"`
	// CreditCode is the mainland unified social credit code, which may be empty when unavailable.
	CreditCode string `json:"creditCode"`
	// RegistrationStatus is the provider's enterprise registration status text.
	RegistrationStatus string `json:"registrationStatus"`
}

// GetEnterpriseBasicDetailsOperation reads one enterprise registration record.
type GetEnterpriseBasicDetailsOperation struct{ client *Client }

// Definition returns the generated enterprise lookup contract.
func (GetEnterpriseBasicDetailsOperation) Definition() sdkgo.QueryDefinition {
	return GetEnterpriseBasicDetailsDefinition
}

// Invoke signs a lookup and classifies provider and transport outcomes.
func (operation GetEnterpriseBasicDetailsOperation) Invoke(call sdkgo.Call, input GetEnterpriseBasicDetailsInput) sdkgo.QueryAttempt[GetEnterpriseBasicDetailsOutput] {
	if strings.TrimSpace(input.Keyword) == "" || strings.TrimSpace(input.Keyword) != input.Keyword || len(input.Keyword) > 1000 {
		return basicDetailsBranch(GetEnterpriseBasicDetailsBranchDefect, sdkgo.FailureValidation, "keyword is invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredential(credentials.AppKey.Reveal()) || !isValidCredential(credentials.SecretKey.Reveal()) {
		return basicDetailsBranch(GetEnterpriseBasicDetailsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := signQichachaRequest(credentials.AppKey.Reveal(), timestamp, credentials.SecretKey.Reveal())
	// Qichacha requires key as a query parameter beside signed headers: https://openapi.qcc.com/dataApi/410
	query := url.Values{"key": {credentials.AppKey.Reveal()}, "keyword": {input.Keyword}}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/ECIV4/GetBasicDetailsByName?"+query.Encode(), nil)
	if err != nil {
		return basicDetailsBranch(GetEnterpriseBasicDetailsBranchDefect, sdkgo.FailureValidation, "enterprise request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Token", signature)
	request.Header.Set("Timespan", timestamp)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return basicDetailsRetry(sdkgo.FailureAvailability, "Qichacha request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return basicDetailsRetry(sdkgo.FailureRateLimit, "Qichacha rate limited the lookup", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return basicDetailsRetry(sdkgo.FailureAvailability, "Qichacha is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return basicDetailsBranch(GetEnterpriseBasicDetailsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Qichacha response exceeded the configured limit")
	}
	if err != nil {
		return basicDetailsRetry(sdkgo.FailureAvailability, "Qichacha response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return basicDetailsBranch(GetEnterpriseBasicDetailsBranchProviderRejected, sdkgo.FailureProviderRejection, "Qichacha rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Status string `json:"Status"`
		Result *struct {
			Name       string `json:"Name"`
			Status     string `json:"Status"`
			CreditCode string `json:"CreditCode"`
			EntType    string `json:"EntType"`
		} `json:"Result"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return basicDetailsBranch(GetEnterpriseBasicDetailsBranchInvalidResponse, sdkgo.FailureProtocol, "Qichacha returned invalid JSON")
	}
	if wire.Status != "200" {
		if wire.Status == "" {
			return basicDetailsBranch(GetEnterpriseBasicDetailsBranchInvalidResponse, sdkgo.FailureProtocol, "Qichacha response has no status")
		}
		return basicDetailsBranch(GetEnterpriseBasicDetailsBranchProviderRejected, sdkgo.FailureProviderRejection, "Qichacha rejected the lookup")
	}
	if wire.Result == nil || wire.Result.Name == "" || wire.Result.Status == "" || len(wire.Result.Name) > 1000 || len(wire.Result.Status) > 100 || len(wire.Result.CreditCode) > 50 {
		return basicDetailsBranch(GetEnterpriseBasicDetailsBranchInvalidResponse, sdkgo.FailureProtocol, "Qichacha response lacks enterprise fields")
	}
	// CreditCode means a different identifier for Hong Kong companies, so exclude non-mainland records.
	if wire.Result.EntType != "0" && wire.Result.EntType != "1" && wire.Result.EntType != "4" && wire.Result.EntType != "7" && wire.Result.EntType != "9" && wire.Result.EntType != "10" && wire.Result.EntType != "11" {
		return basicDetailsBranch(GetEnterpriseBasicDetailsBranchProviderRejected, sdkgo.FailureProviderRejection, "Qichacha returned a non-mainland entity")
	}
	output := GetEnterpriseBasicDetailsOutput{Name: wire.Result.Name, CreditCode: wire.Result.CreditCode, RegistrationStatus: wire.Result.Status}
	return sdkgo.NewQueryBranch(GetEnterpriseBasicDetailsBranchFetched, output, nil, sdkgo.Receipt{})
}

func signQichachaRequest(appKey, timestamp, secretKey string) string {
	return fmt.Sprintf("%X", md5.Sum([]byte(appKey+timestamp+secretKey)))
}

func isValidCredential(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}

func basicDetailsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[GetEnterpriseBasicDetailsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "qichacha", Operation: "getEnterpriseBasicDetails", Message: message}
	return sdkgo.NewQueryBranch(branch, GetEnterpriseBasicDetailsOutput{}, &failure, sdkgo.Receipt{})
}
func basicDetailsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[GetEnterpriseBasicDetailsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "qichacha", Operation: "getEnterpriseBasicDetails", Message: message}
	return sdkgo.NewQueryRetry[GetEnterpriseBasicDetailsOutput](failure, delay)
}
