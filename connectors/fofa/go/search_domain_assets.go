// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fofa

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SearchDomainAssetsInput identifies one DNS domain to search in FOFA's public index.
type SearchDomainAssetsInput struct {
	// Domain is one DNS domain, without a URL scheme or wildcard.
	Domain string `json:"domain"`
}

// DomainAsset is one public host/IP/port record.
type DomainAsset struct {
	// Host is the indexed hostname or URL.
	Host string `json:"host"`
	// IPAddress is the indexed IPv4 or IPv6 address.
	IPAddress string `json:"ipAddress"`
	// Port is the indexed TCP or UDP port.
	Port int `json:"port"`
}

// SearchDomainAssetsOutput contains the total matches and up to five assets.
type SearchDomainAssetsOutput struct {
	// Total is FOFA's reported count for the domain.
	Total int `json:"total"`
	// Assets are at most five records from the first page.
	Assets []DomainAsset `json:"assets"`
}

// SearchDomainAssetsOperation performs a read-only domain asset query that may consume F-points.
type SearchDomainAssetsOperation struct{ client *Client }

// Definition returns the generated search contract.
func (SearchDomainAssetsOperation) Definition() sdkgo.QueryDefinition {
	return SearchDomainAssetsDefinition
}

// Invoke searches the indexed assets for one exact domain.
func (operation SearchDomainAssetsOperation) Invoke(call sdkgo.Call, input SearchDomainAssetsInput) sdkgo.QueryAttempt[SearchDomainAssetsOutput] {
	if !validDomain(input.Domain) {
		return searchBranch(SearchDomainAssetsBranchDefect, sdkgo.FailureValidation, "FOFA domain must be a valid DNS name")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return searchBranch(SearchDomainAssetsBranchDefect, sdkgo.FailureAuthentication, "FOFA API Key is unavailable or invalid")
	}
	endpoint, _ := url.Parse(assetSearchEndpoint)
	values := endpoint.Query()
	values.Set("key", credentials.APIKey.Reveal())
	values.Set("qbase64", base64.StdEncoding.EncodeToString([]byte(`domain="`+input.Domain+`"`)))
	values.Set("fields", "host,ip,port")
	values.Set("page", "1")
	values.Set("size", "5")
	values.Set("r_type", "json")
	endpoint.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return searchBranch(SearchDomainAssetsBranchDefect, sdkgo.FailureValidation, "FOFA search request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return searchRetry(sdkgo.FailureAvailability, "FOFA search request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return searchRetry(sdkgo.FailureRateLimit, "FOFA rate limited search", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return searchRetry(sdkgo.FailureAvailability, "FOFA search service is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return classifySearchHTTPStatus(response.StatusCode)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return searchBranch(SearchDomainAssetsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "FOFA response exceeded the configured limit")
	}
	if err != nil {
		return searchRetry(sdkgo.FailureAvailability, "FOFA response was interrupted", 0)
	}
	var wire struct {
		Error   *bool      `json:"error"`
		Errmsg  string     `json:"errmsg"`
		Size    *int       `json:"size"`
		Results [][]string `json:"results"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Error == nil {
		return searchBranch(SearchDomainAssetsBranchInvalidResponse, sdkgo.FailureProtocol, "FOFA returned invalid search data")
	}
	if *wire.Error {
		return searchBranch(SearchDomainAssetsBranchProviderRejected, sdkgo.FailureProviderRejection, "FOFA rejected the search")
	}
	if wire.Size == nil || *wire.Size < 0 || wire.Results == nil || len(wire.Results) > 5 {
		return searchBranch(SearchDomainAssetsBranchInvalidResponse, sdkgo.FailureProtocol, "FOFA returned invalid search results")
	}
	assets := make([]DomainAsset, 0, len(wire.Results))
	for _, row := range wire.Results {
		if len(row) != 3 || row[0] == "" || len(row[0]) > 2048 || net.ParseIP(row[1]) == nil {
			return searchBranch(SearchDomainAssetsBranchInvalidResponse, sdkgo.FailureProtocol, "FOFA returned invalid asset data")
		}
		port, err := strconv.Atoi(row[2])
		if err != nil || port < 1 || port > 65535 {
			return searchBranch(SearchDomainAssetsBranchInvalidResponse, sdkgo.FailureProtocol, "FOFA returned invalid asset port")
		}
		assets = append(assets, DomainAsset{Host: row[0], IPAddress: row[1], Port: port})
	}
	return sdkgo.NewQueryBranch(SearchDomainAssetsBranchFound, SearchDomainAssetsOutput{Total: *wire.Size, Assets: assets}, nil, sdkgo.Receipt{})
}

func validDomain(domain string) bool {
	if domain == "" || len(domain) > 253 || strings.ToLower(domain) != domain || strings.HasSuffix(domain, ".") || !strings.Contains(domain, ".") {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if character != '-' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
				return false
			}
		}
	}
	return true
}
func classifySearchHTTPStatus(status int) sdkgo.QueryAttempt[SearchDomainAssetsOutput] {
	kind := sdkgo.FailureProviderRejection
	if status == http.StatusUnauthorized {
		kind = sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		kind = sdkgo.FailureAuthorization
	}
	return searchBranch(SearchDomainAssetsBranchProviderRejected, kind, "FOFA rejected search with HTTP "+strconv.Itoa(status))
}
func searchBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[SearchDomainAssetsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fofa", Operation: "searchDomainAssets", Message: message}
	return sdkgo.NewQueryBranch(branch, SearchDomainAssetsOutput{}, &failure, sdkgo.Receipt{})
}
func searchRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[SearchDomainAssetsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "fofa", Operation: "searchDomainAssets", Message: message}
	return sdkgo.NewQueryRetry[SearchDomainAssetsOutput](failure, delay)
}
