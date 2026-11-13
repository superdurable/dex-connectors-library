// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lixinger

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const stockCompaniesPath = "/api/cn/company"

var stockCodePattern = regexp.MustCompile(`^[0-9]{6}$`)

// ListStockCompaniesInput selects one to one hundred A-share stock codes.
type ListStockCompaniesInput struct {
	// StockCodes are six-digit A-share identifiers to look up, without exchange prefixes.
	StockCodes []string `json:"stockCodes"`
}

// StockCompany contains only basic listing identifiers, without prices or financial metrics.
type StockCompany struct {
	// StockCode is the six-digit A-share code.
	StockCode string `json:"stockCode"`
	// Name is the company's exchange listing name.
	Name string `json:"name"`
	// Exchange is the provider's exchange code such as sh, sz, or bj.
	Exchange string `json:"exchange"`
	// ListingStatus is the provider's listing-state code, blank when omitted.
	ListingStatus string `json:"listingStatus"`
}

// ListStockCompaniesOutput contains the returned matching companies.
type ListStockCompaniesOutput struct {
	// Companies are returned in provider order.
	Companies []StockCompany `json:"companies"`
}

// ListStockCompaniesOperation reads A-share company metadata.
type ListStockCompaniesOperation struct{ client *Client }

// Definition returns the generated company metadata Query contract.
func (ListStockCompaniesOperation) Definition() sdkgo.QueryDefinition {
	return ListStockCompaniesDefinition
}

// Invoke retrieves company identifiers and classifies provider and transport outcomes.
func (operation ListStockCompaniesOperation) Invoke(call sdkgo.Call, input ListStockCompaniesInput) sdkgo.QueryAttempt[ListStockCompaniesOutput] {
	if len(input.StockCodes) < 1 || len(input.StockCodes) > 100 {
		return companyBranch(ListStockCompaniesBranchDefect, sdkgo.FailureValidation, "stockCodes must contain one to one hundred codes")
	}
	requested := make(map[string]struct{}, len(input.StockCodes))
	for _, code := range input.StockCodes {
		if !stockCodePattern.MatchString(code) {
			return companyBranch(ListStockCompaniesBranchDefect, sdkgo.FailureValidation, "stockCodes must contain six-digit codes")
		}
		if _, exists := requested[code]; exists {
			return companyBranch(ListStockCompaniesBranchDefect, sdkgo.FailureValidation, "stockCodes must not repeat")
		}
		requested[code] = struct{}{}
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidToken(credentials.Token.Reveal()) {
		return companyBranch(ListStockCompaniesBranchDefect, sdkgo.FailureAuthentication, "Lixinger token is unavailable or invalid")
	}
	payload, _ := json.Marshal(struct {
		StockCodes []string `json:"stockCodes"`
		Token      string   `json:"token"`
	}{input.StockCodes, credentials.Token.Reveal()})
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+stockCompaniesPath, strings.NewReader(string(payload)))
	if err != nil {
		return companyBranch(ListStockCompaniesBranchDefect, sdkgo.FailureValidation, "company request is invalid")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return companyRetry(sdkgo.FailureAvailability, "Lixinger request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return companyRetry(sdkgo.FailureRateLimit, "Lixinger rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return companyRetry(sdkgo.FailureAvailability, "Lixinger temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return companyBranch(ListStockCompaniesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "company response exceeded the configured limit")
	}
	if err != nil {
		return companyRetry(sdkgo.FailureAvailability, "company response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return companyBranch(ListStockCompaniesBranchProviderRejected, kind, "Lixinger rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var wire struct {
		Code *int `json:"code"`
		Data []struct {
			StockCode     string `json:"stockCode"`
			Name          string `json:"name"`
			Exchange      string `json:"exchange"`
			ListingStatus string `json:"listingStatus"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	if decoder.Decode(&wire) != nil || wire.Code == nil {
		return companyBranch(ListStockCompaniesBranchInvalidResponse, sdkgo.FailureProtocol, "Lixinger returned invalid company data")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return companyBranch(ListStockCompaniesBranchInvalidResponse, sdkgo.FailureProtocol, "Lixinger returned trailing company data")
	}
	if *wire.Code != 1 {
		return companyBranch(ListStockCompaniesBranchProviderRejected, sdkgo.FailureProviderRejection, "Lixinger rejected the request, code "+strconv.Itoa(*wire.Code))
	}
	if wire.Data == nil || len(wire.Data) > len(input.StockCodes) {
		return companyBranch(ListStockCompaniesBranchInvalidResponse, sdkgo.FailureProtocol, "Lixinger returned invalid company count")
	}
	companies := make([]StockCompany, 0, len(wire.Data))
	seen := make(map[string]struct{}, len(wire.Data))
	for _, item := range wire.Data {
		if _, exists := requested[item.StockCode]; !exists || item.Name == "" || len(item.Name) > 512 || !isExchange(item.Exchange) || len(item.ListingStatus) > 128 {
			return companyBranch(ListStockCompaniesBranchInvalidResponse, sdkgo.FailureProtocol, "Lixinger returned invalid company metadata")
		}
		if _, exists := seen[item.StockCode]; exists {
			return companyBranch(ListStockCompaniesBranchInvalidResponse, sdkgo.FailureProtocol, "Lixinger returned duplicate company metadata")
		}
		seen[item.StockCode] = struct{}{}
		companies = append(companies, StockCompany{StockCode: item.StockCode, Name: item.Name, Exchange: item.Exchange, ListingStatus: item.ListingStatus})
	}
	return sdkgo.NewQueryBranch(ListStockCompaniesBranchListed, ListStockCompaniesOutput{Companies: companies}, nil, sdkgo.Receipt{})
}
func isExchange(value string) bool { return value == "sh" || value == "sz" || value == "bj" }
func isValidToken(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func companyBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListStockCompaniesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "lixinger", Operation: "listStockCompanies", Message: message}
	return sdkgo.NewQueryBranch(branch, ListStockCompaniesOutput{}, &failure, sdkgo.Receipt{})
}
func companyRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListStockCompaniesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "lixinger", Operation: "listStockCompanies", Message: message}
	return sdkgo.NewQueryRetry[ListStockCompaniesOutput](failure, delay)
}
