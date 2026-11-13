// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package west

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// ListDomainsInput selects one reseller domain page. Zero page and limit use 1 and 20.
type ListDomainsInput struct {
	// Page is the one-based page number; zero selects the first page.
	Page int `json:"page"`
	// Limit is the number of domains to request, from 1 to 100; zero uses 20.
	Limit int `json:"limit"`
}

// Domain contains non-personal registrar metadata.
type Domain struct {
	// Name is the registered domain name.
	Name string `json:"name"`
	// ExpiresAt is the provider's expiration date text.
	ExpiresAt string `json:"expiresAt"`
}

// ListDomainsOutput contains one domain page and provider pagination counts.
type ListDomainsOutput struct {
	// Domains are the returned domains in provider order.
	Domains []Domain `json:"domains"`
	// Page is the returned one-based page number.
	Page int `json:"page"`
	// TotalPages is the total number of pages reported by West.cn.
	TotalPages int `json:"totalPages"`
	// Total is the total number of domains reported by West.cn.
	Total int `json:"total"`
}

// ListDomainsOperation reads a West.cn reseller domain page.
type ListDomainsOperation struct{ client *Client }

// Definition returns the generated domain-list contract.
func (ListDomainsOperation) Definition() sdkgo.QueryDefinition { return ListDomainsDefinition }

// Invoke reads one domain page and classifies provider and transport outcomes.
func (operation ListDomainsOperation) Invoke(call sdkgo.Call, input ListDomainsInput) sdkgo.QueryAttempt[ListDomainsOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidCredentialPart(credentials.Username) || !isValidCredentialPart(credentials.APIPassword.Reveal()) {
		return domainCatalogBranch(ListDomainsBranchDefect, sdkgo.FailureAuthentication, "reseller credentials are unavailable or invalid")
	}
	page, limit := input.Page, input.Limit
	if page == 0 {
		page = 1
	}
	if limit == 0 {
		limit = 20
	}
	if page < 1 || page > 1000000 || limit < 1 || limit > 100 {
		return domainCatalogBranch(ListDomainsBranchDefect, sdkgo.FailureValidation, "page or limit is outside the supported range")
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	parameters := url.Values{}
	parameters.Set("act", "getdomains")
	parameters.Set("username", credentials.Username)
	parameters.Set("time", timestamp)
	parameters.Set("token", signWestToken(credentials.Username, credentials.APIPassword.Reveal(), timestamp))
	parameters.Set("page", strconv.Itoa(page))
	parameters.Set("limit", strconv.Itoa(limit))
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/api/v2/domain/?"+parameters.Encode(), nil)
	if err != nil {
		return domainCatalogBranch(ListDomainsBranchDefect, sdkgo.FailureValidation, "domain-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return domainCatalogRetry(sdkgo.FailureAvailability, "West.cn request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return domainCatalogRetry(sdkgo.FailureRateLimit, "West.cn rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return domainCatalogRetry(sdkgo.FailureAvailability, "West.cn is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return domainCatalogBranch(ListDomainsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "West.cn response exceeded the configured limit")
	}
	if err != nil {
		return domainCatalogRetry(sdkgo.FailureAvailability, "West.cn response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return domainCatalogBranch(ListDomainsBranchProviderRejected, kind, "West.cn rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(body)
	if err != nil {
		return domainCatalogBranch(ListDomainsBranchInvalidResponse, sdkgo.FailureProtocol, "West.cn returned invalid GBK data")
	}
	var wire struct {
		Result *int `json:"result"`
		Data   *struct {
			Items []struct {
				Domain  string `json:"domain"`
				ExpDate string `json:"expdate"`
			} `json:"items"`
			PageNo     int `json:"pageno"`
			TotalPages int `json:"totalpages"`
			Total      int `json:"total"`
		} `json:"data"`
	}
	if json.Unmarshal(decoded, &wire) != nil || wire.Result == nil {
		return domainCatalogBranch(ListDomainsBranchInvalidResponse, sdkgo.FailureProtocol, "West.cn returned invalid domain-list data")
	}
	if *wire.Result != 200 {
		return domainCatalogBranch(ListDomainsBranchProviderRejected, sdkgo.FailureProviderRejection, "West.cn rejected the domain-list request")
	}
	if wire.Data == nil || len(wire.Data.Items) > limit || wire.Data.PageNo < 0 || wire.Data.TotalPages < 0 || wire.Data.Total < 0 {
		return domainCatalogBranch(ListDomainsBranchInvalidResponse, sdkgo.FailureProtocol, "West.cn returned invalid domain-list data")
	}
	domains := make([]Domain, 0, len(wire.Data.Items))
	for _, item := range wire.Data.Items {
		if item.Domain == "" || len(item.Domain) > 253 || strings.TrimSpace(item.Domain) != item.Domain || item.ExpDate == "" || len(item.ExpDate) > 64 {
			return domainCatalogBranch(ListDomainsBranchInvalidResponse, sdkgo.FailureProtocol, "West.cn returned an invalid domain")
		}
		domains = append(domains, Domain{Name: item.Domain, ExpiresAt: item.ExpDate})
	}
	return sdkgo.NewQueryBranch(ListDomainsBranchListed, ListDomainsOutput{Domains: domains, Page: wire.Data.PageNo, TotalPages: wire.Data.TotalPages, Total: wire.Data.Total}, nil, sdkgo.Receipt{})
}

func signWestToken(username, apiPassword, timestamp string) string {
	digest := md5.Sum([]byte(username + apiPassword + timestamp))
	return hex.EncodeToString(digest[:])
}
func isValidCredentialPart(value string) bool {
	if value == "" || len(value) > 512 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}
func domainCatalogBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListDomainsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "west", Operation: "listDomains", Message: message}
	return sdkgo.NewQueryBranch(branch, ListDomainsOutput{}, &failure, sdkgo.Receipt{})
}
func domainCatalogRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListDomainsOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "west", Operation: "listDomains", Message: message}
	return sdkgo.NewQueryRetry[ListDomainsOutput](failure, delay)
}
