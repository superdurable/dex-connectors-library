// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smzdm

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

// SearchProductsInput selects a bounded first page from the SMZDM product index.
type SearchProductsInput struct {
	// Keyword is the product search term, limited to 100 Unicode characters.
	Keyword string `json:"keyword"`
}

// Product contains the public title and provider-reported price of a product-index entry.
type Product struct {
	// Title is the indexed product title.
	Title string `json:"title"`
	// PriceYuan is the provider-reported product price in Chinese yuan.
	PriceYuan float64 `json:"priceYuan"`
}

// ProductPage is the first page of matching product-index entries.
type ProductPage struct {
	// Total is the provider-reported matching entry count.
	Total int `json:"total"`
	// Products contains at most 20 items from the first page.
	Products []Product `json:"products"`
}

// SearchProductsOperation reads the first page of product-index entries.
type SearchProductsOperation struct{ client *Client }

// Definition returns the generated product-search contract.
func (SearchProductsOperation) Definition() sdkgo.QueryDefinition { return SearchProductsDefinition }

// Invoke queries SMZDM and classifies provider, protocol, and transport outcomes.
func (operation SearchProductsOperation) Invoke(call sdkgo.Call, input SearchProductsInput) sdkgo.QueryAttempt[ProductPage] {
	if !validKeyword(input.Keyword) {
		return productBranch(SearchProductsBranchDefect, sdkgo.FailureValidation, "SMZDM keyword must contain 1 to 100 printable characters")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validCredential(credentials.AppKey.Reveal()) || !validCredential(credentials.AppSecret.Reveal()) {
		return productBranch(SearchProductsBranchDefect, sdkgo.FailureAuthentication, "SMZDM App Key or App Secret is unavailable or invalid")
	}
	parameters := map[string]string{
		"app_key": credentials.AppKey.Reveal(), "c": "new_wiki", "offset": "0", "request_from": operation.client.requestFrom,
		"s": input.Keyword, "size": "20", "timestamp": strconv.FormatInt(time.Now().Unix(), 10),
	}
	query := url.Values{}
	for key, value := range parameters {
		query.Set(key, value)
	}
	query.Set("sign", signProducts(parameters, credentials.AppSecret.Reveal()))
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v1/searchapi/search/list?"+query.Encode(), nil)
	if err != nil {
		return productBranch(SearchProductsBranchDefect, sdkgo.FailureValidation, "product-search request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return productRetry(sdkgo.FailureAvailability, "SMZDM product-search request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return productRetry(sdkgo.FailureRateLimit, "SMZDM rate limited product search", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return productRetry(sdkgo.FailureAvailability, "SMZDM product search is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return productBranch(SearchProductsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "SMZDM response exceeded the configured limit")
	}
	if err != nil {
		return productRetry(sdkgo.FailureAvailability, "SMZDM response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return productBranch(SearchProductsBranchProviderRejected, classifyProductHTTP(response.StatusCode), "SMZDM rejected product search with HTTP "+strconv.Itoa(response.StatusCode))
	}
	page, status, err := decodeProductPage(body)
	if err != nil {
		return productBranch(SearchProductsBranchInvalidResponse, sdkgo.FailureProtocol, "SMZDM returned an invalid product-search response")
	}
	if !status {
		return productBranch(SearchProductsBranchProviderRejected, sdkgo.FailureProviderRejection, "SMZDM rejected product search")
	}
	return sdkgo.NewQueryBranch(SearchProductsBranchListed, page, nil, sdkgo.Receipt{})
}

func signProducts(parameters map[string]string, secret string) string {
	keys := make([]string, 0, len(parameters))
	for key, value := range parameters {
		if value != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var message strings.Builder
	message.WriteString(secret)
	for _, key := range keys {
		message.WriteString(key)
		message.WriteString(parameters[key])
	}
	message.WriteString(secret)
	digest := md5.Sum([]byte(message.String()))
	return strings.ToUpper(hex.EncodeToString(digest[:]))
}
func decodeProductPage(body []byte) (ProductPage, bool, error) {
	var wire struct {
		Status *bool `json:"status"`
		Data   *struct {
			Total *int `json:"total_num"`
			Rows  []struct {
				Title string   `json:"title"`
				Price *float64 `json:"pro_price"`
			} `json:"rows"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Status == nil {
		return ProductPage{}, false, errors.New("invalid SMZDM JSON")
	}
	if !*wire.Status {
		return ProductPage{}, false, nil
	}
	if wire.Data == nil || wire.Data.Total == nil || *wire.Data.Total < 0 || wire.Data.Rows == nil || len(wire.Data.Rows) > 20 {
		return ProductPage{}, false, errors.New("invalid SMZDM product page")
	}
	page := ProductPage{Total: *wire.Data.Total, Products: make([]Product, 0, len(wire.Data.Rows))}
	for _, item := range wire.Data.Rows {
		if item.Title == "" || item.Price == nil || *item.Price < 0 {
			return ProductPage{}, false, errors.New("invalid SMZDM product")
		}
		page.Products = append(page.Products, Product{Title: item.Title, PriceYuan: *item.Price})
	}
	return page, true, nil
}
func validKeyword(keyword string) bool {
	if keyword == "" || keyword != strings.TrimSpace(keyword) || !utf8.ValidString(keyword) || utf8.RuneCountInString(keyword) > 100 {
		return false
	}
	for _, character := range keyword {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
func validRequestFrom(source string) bool {
	if source == "" || len(source) > 128 || source != strings.TrimSpace(source) {
		return false
	}
	for _, character := range source {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
func validCredential(value string) bool {
	if value == "" || len(value) > 2048 || value != strings.TrimSpace(value) {
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
func productBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ProductPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "smzdm", Operation: "searchProducts", Message: message}
	return sdkgo.NewQueryBranch(branch, ProductPage{}, &failure, sdkgo.Receipt{})
}
func productRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ProductPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "smzdm", Operation: "searchProducts", Message: message}
	return sdkgo.NewQueryRetry[ProductPage](failure, delay)
}
