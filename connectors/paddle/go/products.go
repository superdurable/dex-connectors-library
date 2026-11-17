// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paddle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ProductIDInput identifies one Paddle catalog product.
type ProductIDInput struct {
	// ProductID is a pro_ ID from Catalog > Products or an API response.
	ProductID string `json:"productId"`
}

// ListProductsInput selects one page of products.
type ListProductsInput struct {
	// PerPage is the page size from 1 through 200.
	PerPage int `json:"perPage"`
	// After is the optional pro_ cursor from the previous page.
	After string `json:"after,omitempty"`
}

// CreateProductInput specifies a reusable Paddle catalog product.
type CreateProductInput struct {
	// Name is a 1–200 character product name.
	Name string `json:"name"`
	// TaxCategory is an enabled Paddle tax category, such as standard or saas.
	TaxCategory string `json:"taxCategory"`
	// Description is an optional short product description.
	Description string `json:"description,omitempty"`
}

// UpdateProductInput replaces a product's name and description.
type UpdateProductInput struct {
	// ProductID identifies the product to update.
	ProductID string `json:"productId"`
	// Name is the replacement product name.
	Name string `json:"name"`
	// Description is the replacement description; blank clears it.
	Description string `json:"description"`
}

// PaddleProduct contains catalog identity, tax category, and status.
type PaddleProduct struct {
	// ID is the Paddle product ID.
	ID string `json:"id"`
	// Name is the product name.
	Name string `json:"name"`
	// TaxCategory is the product's tax category.
	TaxCategory string `json:"taxCategory"`
	// Status is active or archived.
	Status string `json:"status"`
}

// ProductPage is one page of products.
type ProductPage struct {
	// Items are products on this page.
	Items []PaddleProduct `json:"items"`
	// HasMore reports whether another page follows.
	HasMore bool `json:"hasMore"`
	// NextCursor is the next page's pro_ ID or blank after the last page.
	NextCursor string `json:"nextCursor"`
}

// ListProductsOperation lists products.
type ListProductsOperation struct{ client *Client }

// GetProductOperation reads one product.
type GetProductOperation struct{ client *Client }

// CreateProductOperation creates one product.
type CreateProductOperation struct{ client *Client }

// UpdateProductOperation replaces a product name and description.
type UpdateProductOperation struct{ client *Client }

// ArchiveProductOperation archives a product.
type ArchiveProductOperation struct{ client *Client }

// ListProducts binds the listing Query.
func (client *Client) ListProducts() ListProductsOperation { return ListProductsOperation{client} }

// GetProduct binds the lookup Query.
func (client *Client) GetProduct() GetProductOperation { return GetProductOperation{client} }

// CreateProduct binds the creation Mutation.
func (client *Client) CreateProduct() CreateProductOperation { return CreateProductOperation{client} }

// UpdateProduct binds the update Mutation.
func (client *Client) UpdateProduct() UpdateProductOperation { return UpdateProductOperation{client} }

// ArchiveProduct binds the archive Mutation.
func (client *Client) ArchiveProduct() ArchiveProductOperation {
	return ArchiveProductOperation{client}
}

// Definition returns the generated listing contract.
func (ListProductsOperation) Definition() sdkgo.QueryDefinition { return ListProductsDefinition }

// Definition returns the generated lookup contract.
func (GetProductOperation) Definition() sdkgo.QueryDefinition { return GetProductDefinition }

// Definition returns the generated creation contract.
func (CreateProductOperation) Definition() sdkgo.MutationDefinition { return CreateProductDefinition }

// Definition returns the generated update contract.
func (UpdateProductOperation) Definition() sdkgo.MutationDefinition { return UpdateProductDefinition }

// Definition returns the generated archive contract.
func (ArchiveProductOperation) Definition() sdkgo.MutationDefinition { return ArchiveProductDefinition }

// IdempotencyKey records call identity; Paddle does not deduplicate product creation on this key.
func (CreateProductOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateProductInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records update call identity.
func (UpdateProductOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateProductInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records archive call identity.
func (ArchiveProductOperation) IdempotencyKey(callID sdkgo.CallID, _ ProductIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

func validProductName(name string) bool { return strings.TrimSpace(name) != "" && len(name) <= 200 }

func validProductTaxCategory(category string) bool {
	switch category {
	case "digital-goods", "ebooks", "implementation-services", "professional-services", "saas", "software-programming-services", "standard", "training-services", "website-hosting":
		return true
	default:
		return false
	}
}

func decodePaddleProduct(body []byte, expectedID string) (PaddleProduct, bool) {
	var envelope struct {
		Data struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			TaxCategory string `json:"tax_category"`
			Status      string `json:"status"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return PaddleProduct{}, false
	}
	product := PaddleProduct{ID: envelope.Data.ID, Name: envelope.Data.Name, TaxCategory: envelope.Data.TaxCategory, Status: envelope.Data.Status}
	if !validProduct(product, expectedID) {
		return PaddleProduct{}, false
	}
	return product, true
}

func validProduct(product PaddleProduct, expectedID string) bool {
	return validPaddleID(product.ID, "pro_") && (expectedID == "" || product.ID == expectedID) && validProductName(product.Name) && validProductTaxCategory(product.TaxCategory) && (product.Status == "active" || product.Status == "archived")
}

func decodeProductPage(body []byte) (ProductPage, bool) {
	var envelope struct {
		Data []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			TaxCategory string `json:"tax_category"`
			Status      string `json:"status"`
		} `json:"data"`
		Meta struct {
			Pagination struct {
				Next    string `json:"next"`
				HasMore bool   `json:"has_more"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Data == nil {
		return ProductPage{}, false
	}
	page := ProductPage{Items: make([]PaddleProduct, 0, len(envelope.Data)), HasMore: envelope.Meta.Pagination.HasMore}
	for _, item := range envelope.Data {
		product := PaddleProduct{ID: item.ID, Name: item.Name, TaxCategory: item.TaxCategory, Status: item.Status}
		if !validProduct(product, "") {
			return ProductPage{}, false
		}
		page.Items = append(page.Items, product)
	}
	if page.HasMore {
		next, err := url.Parse(envelope.Meta.Pagination.Next)
		if err != nil || next.Path != "/products" || !validPaddleID(next.Query().Get("after"), "pro_") {
			return ProductPage{}, false
		}
		page.NextCursor = next.Query().Get("after")
	}
	return page, true
}

func productFailure(name string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "paddle", Operation: name, Message: message}
}

// Invoke reads one product page.
func (operation ListProductsOperation) Invoke(call sdkgo.Call, input ListProductsInput) sdkgo.QueryAttempt[ProductPage] {
	const name = "listProducts"
	if input.PerPage < 1 || input.PerPage > 200 || input.After != "" && !validPaddleID(input.After, "pro_") {
		return sdkgo.NewQueryBranch(ListProductsBranchDefect, ProductPage{}, productFailure(name, sdkgo.FailureValidation, "product page input is invalid"), sdkgo.Receipt{})
	}
	query := url.Values{"per_page": {strconv.Itoa(input.PerPage)}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	exchange := operation.client.exchange(call, call.Context, http.MethodGet, "/products?"+query.Encode(), nil)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewQueryBranch(ListProductsBranchDefect, ProductPage{}, productFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewQueryRetry[ProductPage](*productFailure(name, sdkgo.FailureAvailability, "product listing is unavailable"), exchange.retryAfter)
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewQueryBranch(ListProductsBranchProviderRejected, ProductPage{}, productFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected product listing"), sdkgo.Receipt{})
	}
	page, ok := decodeProductPage(exchange.body)
	if !ok || exchange.err != nil {
		return sdkgo.NewQueryBranch(ListProductsBranchInvalidResponse, ProductPage{}, productFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid product page"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(ListProductsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke reads one product.
func (operation GetProductOperation) Invoke(call sdkgo.Call, input ProductIDInput) sdkgo.QueryAttempt[PaddleProduct] {
	const name = "getProduct"
	if !validPaddleID(input.ProductID, "pro_") {
		return sdkgo.NewQueryBranch(GetProductBranchDefect, PaddleProduct{}, productFailure(name, sdkgo.FailureValidation, "product ID is invalid"), sdkgo.Receipt{})
	}
	exchange := operation.client.exchange(call, call.Context, http.MethodGet, "/products/"+url.PathEscape(input.ProductID), nil)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewQueryBranch(GetProductBranchDefect, PaddleProduct{}, productFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewQueryRetry[PaddleProduct](*productFailure(name, sdkgo.FailureAvailability, "product lookup is unavailable"), exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetProductBranchNotFound, PaddleProduct{}, productFailure(name, sdkgo.FailureNotFound, "product was not found"), sdkgo.Receipt{})
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewQueryBranch(GetProductBranchProviderRejected, PaddleProduct{}, productFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected product lookup"), sdkgo.Receipt{})
	}
	product, ok := decodePaddleProduct(exchange.body, input.ProductID)
	if !ok || exchange.err != nil {
		return sdkgo.NewQueryBranch(GetProductBranchInvalidResponse, PaddleProduct{}, productFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid product"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetProductBranchFound, product, nil, sdkgo.Receipt{})
}

// Invoke creates a product with a bounded number of possible sends.
func (operation CreateProductOperation) Invoke(call sdkgo.Call, input CreateProductInput) sdkgo.MutationAttempt[PaddleProduct] {
	const name = "createProduct"
	if !validProductName(input.Name) || !validProductTaxCategory(input.TaxCategory) || len(input.Description) > 2048 {
		return sdkgo.NewMutationBranch(CreateProductBranchDefect, PaddleProduct{}, productFailure(name, sdkgo.FailureValidation, "product name, tax category, or description is invalid"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateProductDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateProductBranchDefect, PaddleProduct{}, productFailure(name, sdkgo.FailureLocalDefect, "product dispatch fence is unavailable"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Marker.ProviderID != "" {
		readBack := operation.client.exchange(call, call.Context, http.MethodGet, "/products/"+url.PathEscape(decision.Marker.ProviderID), nil)
		if readBack.status == http.StatusOK && readBack.err == nil {
			product, ok := decodePaddleProduct(readBack.body, decision.Marker.ProviderID)
			if ok && product.Name == input.Name && product.TaxCategory == input.TaxCategory {
				return sdkgo.NewMutationBranch(CreateProductBranchCreated, product, nil, sdkgo.Receipt{ProviderObjectID: product.ID})
			}
		}
		if paddleRetryable(readBack) {
			return sdkgo.NewMutationRetry[PaddleProduct](*productFailure(name, sdkgo.FailureAvailability, "product read-back is unavailable"), readBack.retryAfter)
		}
		return sdkgo.NewMutationUncertain(PaddleProduct{}, *productFailure(name, sdkgo.FailureProtocol, "created product could not be confirmed"), sdkgo.Receipt{})
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired || decision.MaximumPriorSends >= call.MaxSends() {
		return sdkgo.NewMutationUncertain(PaddleProduct{}, *productFailure(name, sdkgo.FailureTransport, "product creation outcome is unknown"), sdkgo.Receipt{})
	}
	var exchange paddleExchange
	payload := map[string]string{"name": input.Name, "tax_category": input.TaxCategory}
	if input.Description != "" {
		payload["description"] = input.Description
	}
	err = fence.Dispatch(func(ctx context.Context) error {
		exchange = operation.client.exchange(call, ctx, http.MethodPost, "/products", payload)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[PaddleProduct](*productFailure(name, sdkgo.FailureTransport, "product dispatch did not start"), 0)
	}
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewMutationBranch(CreateProductBranchDefect, PaddleProduct{}, productFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if exchange.status == http.StatusTooManyRequests || exchange.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(PaddleProduct{}, *productFailure(name, sdkgo.FailureTransport, "product retry checkpoint failed"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[PaddleProduct](*productFailure(name, sdkgo.FailureAvailability, "Paddle requested a retry"), exchange.retryAfter)
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewMutationRetry[PaddleProduct](*productFailure(name, sdkgo.FailureAvailability, "product creation outcome is unknown"), exchange.retryAfter)
	}
	if exchange.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(PaddleProduct{}, *productFailure(name, sdkgo.FailureTransport, "an earlier product create may have succeeded"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(CreateProductBranchProviderRejected, PaddleProduct{}, productFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected product creation"), sdkgo.Receipt{})
	}
	product, ok := decodePaddleProduct(exchange.body, "")
	if !ok || exchange.err != nil || product.Name != input.Name || product.TaxCategory != input.TaxCategory {
		return sdkgo.NewMutationUncertain(PaddleProduct{}, *productFailure(name, sdkgo.FailureProtocol, "Paddle accepted product creation but returned invalid data"), sdkgo.Receipt{})
	}
	_ = fence.RecordProviderID(product.ID)
	return sdkgo.NewMutationBranch(CreateProductBranchCreated, product, nil, sdkgo.Receipt{ProviderObjectID: product.ID})
}

func (client *Client) replaceProduct(call sdkgo.Call, name, productID string, payload any, expectedStatus string, successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[PaddleProduct] {
	exchange := client.exchange(call, call.Context, http.MethodPatch, "/products/"+url.PathEscape(productID), payload)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewMutationBranch(defectBranch, PaddleProduct{}, productFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewMutationRetry[PaddleProduct](*productFailure(name, sdkgo.FailureAvailability, "product update is unavailable"), exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(notFoundBranch, PaddleProduct{}, productFailure(name, sdkgo.FailureNotFound, "product was not found"), sdkgo.Receipt{})
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewMutationBranch(rejectedBranch, PaddleProduct{}, productFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected product update"), sdkgo.Receipt{})
	}
	product, ok := decodePaddleProduct(exchange.body, productID)
	if !ok || exchange.err != nil || expectedStatus != "" && product.Status != expectedStatus {
		return sdkgo.NewMutationBranch(invalidBranch, PaddleProduct{}, productFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid product"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(successBranch, product, nil, sdkgo.Receipt{ProviderObjectID: product.ID})
}

// Invoke replaces a product's name and description.
func (operation UpdateProductOperation) Invoke(call sdkgo.Call, input UpdateProductInput) sdkgo.MutationAttempt[PaddleProduct] {
	const name = "updateProduct"
	if !validPaddleID(input.ProductID, "pro_") || !validProductName(input.Name) || len(input.Description) > 2048 {
		return sdkgo.NewMutationBranch(UpdateProductBranchDefect, PaddleProduct{}, productFailure(name, sdkgo.FailureValidation, "product ID, name, or description is invalid"), sdkgo.Receipt{})
	}
	return operation.client.replaceProduct(call, name, input.ProductID, map[string]string{"name": input.Name, "description": input.Description}, "", UpdateProductBranchUpdated, UpdateProductBranchNotFound, UpdateProductBranchProviderRejected, UpdateProductBranchInvalidResponse, UpdateProductBranchDefect)
}

// Invoke archives a product with an absolute status replacement.
func (operation ArchiveProductOperation) Invoke(call sdkgo.Call, input ProductIDInput) sdkgo.MutationAttempt[PaddleProduct] {
	const name = "archiveProduct"
	if !validPaddleID(input.ProductID, "pro_") {
		return sdkgo.NewMutationBranch(ArchiveProductBranchDefect, PaddleProduct{}, productFailure(name, sdkgo.FailureValidation, "product ID is invalid"), sdkgo.Receipt{})
	}
	return operation.client.replaceProduct(call, name, input.ProductID, map[string]string{"status": "archived"}, "archived", ArchiveProductBranchArchived, ArchiveProductBranchNotFound, ArchiveProductBranchProviderRejected, ArchiveProductBranchInvalidResponse, ArchiveProductBranchDefect)
}
