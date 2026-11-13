// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onebound

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const productPageSize = 40

// SearchProductsInput selects public 1688 listings by a keyword and one-based page.
type SearchProductsInput struct {
	// Keyword is a nonempty public product search term, at most 100 runes.
	Keyword string `json:"keyword"`
	// PageNumber is one-based; zero selects the first page.
	PageNumber int `json:"pageNumber"`
}

// ProductSearchItem contains a public listing's product metadata.
type ProductSearchItem struct {
	// ProductID is 1688's product identifier.
	ProductID string `json:"productID"`
	// Title is the public listing title.
	Title string `json:"title"`
	// Price is the provider's displayed price string.
	Price string `json:"price"`
	// DetailURL is the public 1688 product page.
	DetailURL string `json:"detailURL"`
}

// ProductSearchPage contains at most 40 public listings and pagination metadata.
type ProductSearchPage struct {
	// PageNumber is the provider's one-based page number.
	PageNumber int `json:"pageNumber"`
	// Total is the provider's total matching product count.
	Total int `json:"total"`
	// Products contains at most 40 product listings.
	Products []ProductSearchItem `json:"products"`
}

// SearchProductsOperation searches 1688 listings through Onebound.
type SearchProductsOperation struct{ client *Client }

// Definition returns the generated product-search Query contract.
func (SearchProductsOperation) Definition() sdkgo.QueryDefinition { return SearchProductsDefinition }

// Invoke signs an HTTPS search and classifies provider, protocol, and transport outcomes.
func (operation SearchProductsOperation) Invoke(call sdkgo.Call, input SearchProductsInput) sdkgo.QueryAttempt[ProductSearchPage] {
	if !validSearchKeyword(input.Keyword) || input.PageNumber < 0 || input.PageNumber > 100000 {
		return productBranch(SearchProductsBranchDefect, sdkgo.FailureValidation, "Onebound keyword or page is invalid")
	}
	pageNumber := input.PageNumber
	if pageNumber == 0 {
		pageNumber = 1
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validCredential(credentials.APIKey.Reveal()) || !validCredential(credentials.APISecret.Reveal()) {
		return productBranch(SearchProductsBranchDefect, sdkgo.FailureAuthentication, "Onebound key or secret is unavailable or invalid")
	}
	parameters := map[string]string{
		"api_name": "item_search", "key": credentials.APIKey.Reveal(),
		"q": input.Keyword, "page": strconv.Itoa(pageNumber), "page_size": strconv.Itoa(productPageSize),
		"dateline": time.Now().In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05"),
	}
	query := url.Values{}
	for name, value := range parameters {
		if name != "api_name" {
			query.Set(name, value)
		}
	}
	query.Set("sign", signSearchParameters(parameters, credentials.APISecret.Reveal()))
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/1688/item_search/?"+query.Encode(), nil)
	if err != nil {
		return productBranch(SearchProductsBranchDefect, sdkgo.FailureValidation, "Onebound request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return productRetry(sdkgo.FailureAvailability, "Onebound product search request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return productRetry(sdkgo.FailureRateLimit, "Onebound rate limited product search", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return productRetry(sdkgo.FailureAvailability, "Onebound product search is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return productBranch(SearchProductsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Onebound response exceeded the configured limit")
	}
	if err != nil {
		return productRetry(sdkgo.FailureAvailability, "Onebound response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return productBranch(SearchProductsBranchProviderRejected, classifyProductHTTP(response.StatusCode), "Onebound rejected product search with HTTP "+strconv.Itoa(response.StatusCode))
	}
	page, code, err := decodeProductSearchPage(body, pageNumber)
	if err != nil {
		return productBranch(SearchProductsBranchInvalidResponse, sdkgo.FailureProtocol, "Onebound returned an invalid product search response")
	}
	switch code {
	case "0000", "2000":
		return sdkgo.NewQueryBranch(SearchProductsBranchListed, page, nil, sdkgo.Receipt{})
	case "4000", "4001", "4002", "4009", "4017", "5000":
		return productRetry(sdkgo.FailureAvailability, "Onebound temporary error "+code, 0)
	case "4008":
		return productRetry(sdkgo.FailureRateLimit, "Onebound concurrency limit exceeded", 0)
	case "4004", "4005":
		return productBranch(SearchProductsBranchProviderRejected, sdkgo.FailureAuthentication, "Onebound rejected credentials with code "+code)
	case "4006", "4007", "4012", "4013", "4016":
		return productBranch(SearchProductsBranchProviderRejected, sdkgo.FailureAuthorization, "Onebound rejected plan or quota with code "+code)
	default:
		return productBranch(SearchProductsBranchProviderRejected, sdkgo.FailureProviderRejection, "Onebound rejected product search with code "+code)
	}
}

func signSearchParameters(parameters map[string]string, secret string) string {
	if len(secret) != 32 {
		digest := md5.Sum([]byte(secret))
		secret = hex.EncodeToString(digest[:])
	}
	names := make([]string, 0, len(parameters))
	for name := range parameters {
		if name != "secret" && name != "sign" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	pairs := make([]string, 0, len(names))
	for _, name := range names {
		value := parameters[name]
		if value != "" && !strings.HasPrefix(value, "@") {
			pairs = append(pairs, name+"="+value)
		}
	}
	digest := md5.Sum([]byte(strings.Join(pairs, "&") + secret))
	return hex.EncodeToString(digest[:])
}

func decodeProductSearchPage(body []byte, requestedPage int) (ProductSearchPage, string, error) {
	var wire struct {
		ErrorCode string `json:"error_code"`
		Items     *struct {
			Page     string `json:"page"`
			Total    *int   `json:"total_results"`
			Products []struct {
				ID        string `json:"num_iid"`
				Title     string `json:"title"`
				Price     string `json:"price"`
				DetailURL string `json:"detail_url"`
			} `json:"item"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.ErrorCode == "" {
		return ProductSearchPage{}, "", errors.New("invalid Onebound JSON")
	}
	if wire.ErrorCode == "2000" {
		return ProductSearchPage{PageNumber: requestedPage, Products: []ProductSearchItem{}}, wire.ErrorCode, nil
	}
	if wire.ErrorCode != "0000" {
		return ProductSearchPage{}, wire.ErrorCode, nil
	}
	if wire.Items == nil || wire.Items.Total == nil || *wire.Items.Total < 0 || wire.Items.Products == nil || len(wire.Items.Products) > productPageSize {
		return ProductSearchPage{}, "", errors.New("invalid Onebound items")
	}
	pageNumber, err := strconv.Atoi(wire.Items.Page)
	if err != nil || pageNumber < 1 {
		return ProductSearchPage{}, "", errors.New("invalid Onebound page")
	}
	page := ProductSearchPage{PageNumber: pageNumber, Total: *wire.Items.Total, Products: make([]ProductSearchItem, 0, len(wire.Items.Products))}
	for _, product := range wire.Items.Products {
		if product.ID == "" || product.Title == "" || product.Price == "" || product.DetailURL == "" {
			return ProductSearchPage{}, "", errors.New("invalid Onebound product")
		}
		page.Products = append(page.Products, ProductSearchItem{ProductID: product.ID, Title: product.Title, Price: product.Price, DetailURL: product.DetailURL})
	}
	return page, wire.ErrorCode, nil
}

func validSearchKeyword(keyword string) bool {
	if keyword == "" || utf8.RuneCountInString(keyword) > 100 || strings.TrimSpace(keyword) != keyword {
		return false
	}
	for _, character := range keyword {
		if unicode.IsControl(character) || character == '&' || character == '=' || character == '?' || character == '#' {
			return false
		}
	}
	return true
}
func validCredential(value string) bool {
	if value == "" || len(value) > 2048 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character <= 0x20 || character >= 0x7f {
			return false
		}
	}
	return true
}
func classifyProductHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func productBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ProductSearchPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "onebound", Operation: "searchProducts", Message: message}
	return sdkgo.NewQueryBranch(branch, ProductSearchPage{}, &failure, sdkgo.Receipt{})
}
func productRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ProductSearchPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "onebound", Operation: "searchProducts", Message: message}
	return sdkgo.NewQueryRetry[ProductSearchPage](failure, delay)
}
