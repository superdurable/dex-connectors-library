// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zentao

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListProductsInput has no selector; ZenTao REST v1 lists visible products.
type ListProductsInput struct{}

// Product is a ZenTao product without any personnel fields.
type Product struct {
	// ID is the product's integer identifier.
	ID int `json:"id"`
	// Name is the product display name.
	Name string `json:"name"`
	// Code is the product's code, which may be empty.
	Code string `json:"code"`
	// Type is normal, branch, or platform.
	Type string `json:"type"`
	// AccessControl is open or private.
	AccessControl string `json:"accessControl"`
}

// ProductList contains the visible products and total count.
type ProductList struct {
	// Total is ZenTao's reported product count.
	Total int `json:"total"`
	// Products contains only product metadata and no user data.
	Products []Product `json:"products"`
}

// ListProductsOperation lists products from ZenTao REST v1.
type ListProductsOperation struct{ client *Client }

// Definition returns the generated product-list contract.
func (ListProductsOperation) Definition() sdkgo.QueryDefinition { return ListProductsDefinition }

// Invoke reads visible ZenTao product metadata.
func (operation ListProductsOperation) Invoke(call sdkgo.Call, _ ListProductsInput) sdkgo.QueryAttempt[ProductList] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.Token.Reveal()) {
		return productBranch(ListProductsBranchDefect, sdkgo.FailureAuthentication, "ZenTao REST v1 token is unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/api.php/v1/products", nil)
	if err != nil {
		return productBranch(ListProductsBranchDefect, sdkgo.FailureValidation, "ZenTao product-list request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Token", credentials.Token.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return productRetry(sdkgo.FailureAvailability, "ZenTao product-list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return productRetry(sdkgo.FailureRateLimit, "ZenTao rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return productRetry(sdkgo.FailureAvailability, "ZenTao is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return productBranch(ListProductsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "ZenTao response exceeded the configured limit")
	}
	if err != nil {
		return productRetry(sdkgo.FailureAvailability, "ZenTao response was interrupted", 0)
	}
	if response.StatusCode != 200 {
		return productBranch(ListProductsBranchProviderRejected, classifyProductHTTP(response.StatusCode), "ZenTao rejected the query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	products, err := decodeProductList(body)
	if err != nil {
		return productBranch(ListProductsBranchInvalidResponse, sdkgo.FailureProtocol, "ZenTao returned an invalid product-list response")
	}
	return sdkgo.NewQueryBranch(ListProductsBranchListed, products, nil, sdkgo.Receipt{})
}
func decodeProductList(body []byte) (ProductList, error) {
	var wire struct {
		Total    *int `json:"total"`
		Products []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Code string `json:"code"`
			Type string `json:"type"`
			ACL  string `json:"acl"`
		} `json:"products"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Total == nil || wire.Products == nil || *wire.Total < 0 || len(wire.Products) > 1000 {
		return ProductList{}, errors.New("invalid ZenTao product list")
	}
	products := ProductList{Total: *wire.Total, Products: make([]Product, 0, len(wire.Products))}
	for _, item := range wire.Products {
		if item.ID <= 0 || item.Name == "" || !validProductType(item.Type) || !validProductACL(item.ACL) {
			return ProductList{}, errors.New("invalid ZenTao product")
		}
		products.Products = append(products.Products, Product{ID: item.ID, Name: item.Name, Code: item.Code, Type: item.Type, AccessControl: item.ACL})
	}
	return products, nil
}
func validProductType(value string) bool {
	return value == "normal" || value == "branch" || value == "platform"
}
func validProductACL(value string) bool { return value == "open" || value == "private" }
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
func productBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ProductList] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zentao", Operation: "listProducts", Message: message}
	return sdkgo.NewQueryBranch(branch, ProductList{}, &failure, sdkgo.Receipt{})
}
func productRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ProductList] {
	failure := sdkgo.Failure{Kind: kind, Provider: "zentao", Operation: "listProducts", Message: message}
	return sdkgo.NewQueryRetry[ProductList](failure, delay)
}
