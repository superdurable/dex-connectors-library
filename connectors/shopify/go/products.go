// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shopify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// Product is a bounded Shopify product summary without image or variant payloads.
type Product struct {
	// ID is the numeric Shopify product ID.
	ID int64 `json:"id"`
	// Title is the product's display title.
	Title string `json:"title"`
	// BodyHTML is the product description HTML.
	BodyHTML string `json:"body_html,omitempty"`
	// Vendor is the merchant's product vendor.
	Vendor string `json:"vendor,omitempty"`
	// ProductType is the merchant's product type label.
	ProductType string `json:"product_type,omitempty"`
	// Handle is Shopify's URL slug.
	Handle string `json:"handle,omitempty"`
	// Tags is Shopify's comma-separated tag list.
	Tags string `json:"tags,omitempty"`
	// Status is active, draft, or archived.
	Status string `json:"status"`
}

// ProductPage contains one bounded product page and the next cursor.
type ProductPage struct {
	// Products contains the returned product summaries.
	Products []Product `json:"products"`
	// NextPageInfo is blank on the final page.
	NextPageInfo string `json:"nextPageInfo,omitempty"`
}

// GetProductInput identifies one product.
type GetProductInput struct {
	// ProductID is the numeric Shopify product ID.
	ProductID int64 `json:"productId"`
}

// ListProductsInput chooses a product page and optional first-page filters.
type ListProductsInput struct {
	// Limit bounds a page from 1 through 250; zero uses Shopify's default.
	Limit int `json:"limit,omitempty"`
	// PageInfo is an opaque cursor from the previous page.
	PageInfo string `json:"pageInfo,omitempty"`
	// IDs is a comma-separated list of numeric product IDs.
	IDs string `json:"ids,omitempty"`
	// Status is active, draft, or archived; blank omits the filter.
	Status string `json:"status,omitempty"`
	// Vendor filters by the merchant's vendor label.
	Vendor string `json:"vendor,omitempty"`
	// ProductType filters by the merchant's product type label.
	ProductType string `json:"productType,omitempty"`
	// Title filters by product title.
	Title string `json:"title,omitempty"`
}

// CreateProductInput defines the first version of a product.
type CreateProductInput struct {
	// Title is the required nonblank product title.
	Title string `json:"title"`
	// BodyHTML is optional product description HTML.
	BodyHTML string `json:"bodyHtml,omitempty"`
	// Vendor is an optional vendor label.
	Vendor string `json:"vendor,omitempty"`
	// ProductType is an optional product type label.
	ProductType string `json:"productType,omitempty"`
	// Tags is an optional comma-separated tag list.
	Tags string `json:"tags,omitempty"`
	// Status is active, draft, or archived; blank uses Shopify's default.
	Status string `json:"status,omitempty"`
}

// UpdateProductInput replaces only the product fields whose pointers are set.
type UpdateProductInput struct {
	// ProductID is the numeric Shopify product ID.
	ProductID int64 `json:"productId"`
	// Title replaces the product title when set.
	Title *string `json:"title,omitempty"`
	// BodyHTML replaces the product description when set; blank clears it.
	BodyHTML *string `json:"bodyHtml,omitempty"`
	// Vendor replaces the vendor label when set; blank clears it.
	Vendor *string `json:"vendor,omitempty"`
	// ProductType replaces the product type when set; blank clears it.
	ProductType *string `json:"productType,omitempty"`
	// Tags replaces the tag list when set; blank clears it.
	Tags *string `json:"tags,omitempty"`
	// Status replaces the product status when set.
	Status *string `json:"status,omitempty"`
}

// DeleteProductInput identifies the product to delete.
type DeleteProductInput struct {
	// ProductID is the numeric Shopify product ID.
	ProductID int64 `json:"productId"`
}

// DeletedProduct identifies the removed product.
type DeletedProduct struct {
	// ProductID is the removed Shopify product ID.
	ProductID int64 `json:"productId"`
}

// GetProductOperation reads one product.
type GetProductOperation struct{ client *Client }

// GetProduct binds the operation to this client.
func (client *Client) GetProduct() GetProductOperation { return GetProductOperation{client} }

// Definition returns the generated getProduct contract.
func (GetProductOperation) Definition() sdkgo.QueryDefinition { return GetProductDefinition }

// Invoke reads a product by ID.
func (operation GetProductOperation) Invoke(call sdkgo.Call, input GetProductInput) sdkgo.QueryAttempt[Product] {
	if input.ProductID <= 0 {
		return failedAttempt[Product]("getProduct", GetProductBranchDefect, sdkgo.FailureValidation, "product ID must be positive")
	}
	return executeRead(operation.client, call, "getProduct", productPath(input.ProductID), nil, GetProductBranchFound, GetProductBranchNotFound, GetProductBranchProviderRejected, GetProductBranchInvalidResponse, GetProductBranchDefect, decodeGetProductResponse)
}

func decodeGetProductResponse(body []byte, _ http.Header) (Product, error) {
	var response struct {
		Product Product `json:"product"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Product.ID <= 0 || response.Product.Title == "" {
		return Product{}, fmt.Errorf("missing product")
	}
	return response.Product, nil
}

// ListProductsOperation reads one product page.
type ListProductsOperation struct{ client *Client }

// ListProducts binds the operation to this client.
func (client *Client) ListProducts() ListProductsOperation { return ListProductsOperation{client} }

// Definition returns the generated listProducts contract.
func (ListProductsOperation) Definition() sdkgo.QueryDefinition { return ListProductsDefinition }

// Invoke lists a bounded product page.
func (operation ListProductsOperation) Invoke(call sdkgo.Call, input ListProductsInput) sdkgo.QueryAttempt[ProductPage] {
	if input.Limit < 0 || input.Limit > 250 {
		return failedAttempt[ProductPage]("listProducts", ListProductsBranchDefect, sdkgo.FailureValidation, "limit must be 1 through 250 or zero")
	}
	query := url.Values{}
	if input.PageInfo != "" {
		if len(input.PageInfo) > 2048 || strings.ContainsAny(input.PageInfo, " \r\n") || input.IDs != "" || input.Status != "" || input.Vendor != "" || input.ProductType != "" || input.Title != "" {
			return failedAttempt[ProductPage]("listProducts", ListProductsBranchDefect, sdkgo.FailureValidation, "pageInfo cannot be combined with filters")
		}
		query.Set("page_info", input.PageInfo)
	} else {
		if !isValidNumericIDList(input.IDs) || !isValidProductStatus(input.Status) || len(input.Vendor) > 255 || len(input.ProductType) > 255 || len(input.Title) > 255 {
			return failedAttempt[ProductPage]("listProducts", ListProductsBranchDefect, sdkgo.FailureValidation, "product filters are invalid")
		}
		setOptionalOrderFilter(query, "ids", input.IDs)
		setOptionalOrderFilter(query, "status", input.Status)
		setOptionalOrderFilter(query, "vendor", input.Vendor)
		setOptionalOrderFilter(query, "product_type", input.ProductType)
		setOptionalOrderFilter(query, "title", input.Title)
	}
	if input.Limit > 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	return executeRead(operation.client, call, "listProducts", "/products.json", query, ListProductsBranchListed, ListProductsBranchNotFound, ListProductsBranchProviderRejected, ListProductsBranchInvalidResponse, ListProductsBranchDefect, decodeListProductsResponse)
}

func decodeListProductsResponse(body []byte, headers http.Header) (ProductPage, error) {
	var page ProductPage
	if err := json.Unmarshal(body, &page); err != nil || page.Products == nil {
		return ProductPage{}, fmt.Errorf("missing product page")
	}
	for _, product := range page.Products {
		if product.ID <= 0 || product.Title == "" {
			return ProductPage{}, fmt.Errorf("invalid product")
		}
	}
	page.NextPageInfo = nextPageInfo(headers.Get("Link"))
	return page, nil
}

// CreateProductOperation creates a product with bounded duplicate sends.
type CreateProductOperation struct{ client *Client }

// CreateProduct binds the operation to this client.
func (client *Client) CreateProduct() CreateProductOperation { return CreateProductOperation{client} }

// Definition returns the generated createProduct contract.
func (CreateProductOperation) Definition() sdkgo.MutationDefinition { return CreateProductDefinition }

// IdempotencyKey derives the stable receipt key from the call ID.
func (CreateProductOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateProductInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a product and bounds ambiguous POST resends.
func (operation CreateProductOperation) Invoke(call sdkgo.Call, input CreateProductInput) sdkgo.MutationAttempt[Product] {
	if strings.TrimSpace(input.Title) == "" || len(input.Title) > 255 || len(input.BodyHTML) > 100000 || len(input.Vendor) > 255 || len(input.ProductType) > 255 || len(input.Tags) > 255 || !isValidProductStatus(input.Status) {
		return writeBranch("createProduct", CreateProductBranchDefect, Product{}, sdkgo.FailureValidation, "product fields are invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateProductDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return writeBranch("createProduct", CreateProductBranchDefect, Product{}, sdkgo.FailureLocalDefect, "product dispatch fence could not be built")
	}
	decision := fence.Decide()
	if decision.Marker.ProviderID != "" {
		providerID, parseError := strconv.ParseInt(decision.Marker.ProviderID, 10, 64)
		if parseError != nil || providerID <= 0 {
			return uncertainProduct(call, "created product checkpoint is invalid")
		}
		response, readError := operation.client.doWrite(call.Context, call, http.MethodGet, productPath(providerID), nil)
		if readError != nil || response.status != http.StatusOK {
			return uncertainProduct(call, "previously created product could not be confirmed")
		}
		product, decodeError := decodeGetProductResponse(response.body, nil)
		if decodeError != nil || product.ID != providerID {
			return uncertainProduct(call, "previously created product could not be confirmed")
		}
		return sdkgo.NewMutationBranch(CreateProductBranchCreated, product, nil, writeReceipt(call, decision.Marker.ProviderID))
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return uncertainProduct(call, "an earlier product creation may have succeeded")
	}
	payload, err := json.Marshal(map[string]any{"product": productCreateFields(input)})
	if err != nil {
		return writeBranch("createProduct", CreateProductBranchDefect, Product{}, sdkgo.FailureValidation, "product payload could not be encoded")
	}
	var response writeResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		var requestError error
		response, requestError = operation.client.doWrite(requestContext, call, http.MethodPost, "/products.json", payload)
		return requestError
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[Product](*failureFor("createProduct", sdkgo.FailureAvailability, "product request was not sent"), 0)
	}
	if errors.Is(err, errShopifyCredential) {
		return writeBranch("createProduct", CreateProductBranchDefect, Product{}, sdkgo.FailureAuthentication, "Shopify credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return uncertainProduct(call, "product retry checkpoint failed")
		}
		return sdkgo.NewMutationRetry[Product](*failureFor("createProduct", sdkgo.FailureAvailability, "product creation was not processed"), response.retryAfter)
	}
	if err != nil || response.status >= 500 || response.status == http.StatusRequestTimeout {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[Product](*failureFor("createProduct", sdkgo.FailureTransport, "product creation outcome is unknown"), 0)
		}
		return uncertainProduct(call, "product creation outcome is unknown after bounded sends")
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return uncertainProduct(call, "an earlier product creation may have succeeded")
		}
		return writeBranch("createProduct", CreateProductBranchProviderRejected, Product{}, sdkgo.FailureProviderRejection, "Shopify rejected product creation")
	}
	product, decodeError := decodeGetProductResponse(response.body, nil)
	if decodeError != nil {
		return uncertainProduct(call, "created product response could not be confirmed")
	}
	if fence.RecordProviderID(strconv.FormatInt(product.ID, 10)) != nil {
		return sdkgo.NewMutationRetry[Product](*failureFor("createProduct", sdkgo.FailureAvailability, "product checkpoint failed"), 0)
	}
	return sdkgo.NewMutationBranch(CreateProductBranchCreated, product, nil, writeReceipt(call, strconv.FormatInt(product.ID, 10)))
}

func productCreateFields(input CreateProductInput) map[string]any {
	fields := map[string]any{"title": input.Title}
	setProductField(fields, "body_html", input.BodyHTML)
	setProductField(fields, "vendor", input.Vendor)
	setProductField(fields, "product_type", input.ProductType)
	setProductField(fields, "tags", input.Tags)
	setProductField(fields, "status", input.Status)
	return fields
}
func setProductField(fields map[string]any, name, value string) {
	if value != "" {
		fields[name] = value
	}
}
func isValidProductStatus(value string) bool {
	return value == "" || value == "active" || value == "draft" || value == "archived"
}
func uncertainProduct(call sdkgo.Call, message string) sdkgo.MutationAttempt[Product] {
	return sdkgo.NewMutationUncertain(Product{}, *failureFor("createProduct", sdkgo.FailureTransport, message), writeReceipt(call, ""))
}

// UpdateProductOperation replaces selected product fields.
type UpdateProductOperation struct{ client *Client }

// UpdateProduct binds the operation to this client.
func (client *Client) UpdateProduct() UpdateProductOperation { return UpdateProductOperation{client} }

// Definition returns the generated updateProduct contract.
func (UpdateProductOperation) Definition() sdkgo.MutationDefinition { return UpdateProductDefinition }

// IdempotencyKey derives the stable receipt key from the call ID.
func (UpdateProductOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateProductInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke repeats an absolute replacement of selected fields safely.
func (operation UpdateProductOperation) Invoke(call sdkgo.Call, input UpdateProductInput) sdkgo.MutationAttempt[Product] {
	if input.ProductID <= 0 || input.Title == nil && input.BodyHTML == nil && input.Vendor == nil && input.ProductType == nil && input.Tags == nil && input.Status == nil || input.Title != nil && (strings.TrimSpace(*input.Title) == "" || len(*input.Title) > 255) || input.BodyHTML != nil && len(*input.BodyHTML) > 100000 || input.Vendor != nil && len(*input.Vendor) > 255 || input.ProductType != nil && len(*input.ProductType) > 255 || input.Tags != nil && len(*input.Tags) > 255 || input.Status != nil && (*input.Status == "" || !isValidProductStatus(*input.Status)) {
		return writeBranch("updateProduct", UpdateProductBranchDefect, Product{}, sdkgo.FailureValidation, "product update fields are invalid")
	}
	fields := map[string]any{"id": input.ProductID}
	if input.Title != nil {
		fields["title"] = *input.Title
	}
	if input.BodyHTML != nil {
		fields["body_html"] = *input.BodyHTML
	}
	if input.Vendor != nil {
		fields["vendor"] = *input.Vendor
	}
	if input.ProductType != nil {
		fields["product_type"] = *input.ProductType
	}
	if input.Tags != nil {
		fields["tags"] = *input.Tags
	}
	if input.Status != nil {
		fields["status"] = *input.Status
	}
	payload, err := json.Marshal(map[string]any{"product": fields})
	if err != nil {
		return writeBranch("updateProduct", UpdateProductBranchDefect, Product{}, sdkgo.FailureValidation, "product update could not be encoded")
	}
	branches := writeBranches{success: UpdateProductBranchUpdated, notFound: UpdateProductBranchNotFound, rejected: UpdateProductBranchProviderRejected, invalid: UpdateProductBranchInvalidResponse, defect: UpdateProductBranchDefect}
	return invokeSafeWrite(call, operation.client, "updateProduct", http.MethodPut, productPath(input.ProductID), payload, http.StatusOK, branches, func(body []byte) (Product, error) { return decodeGetProductResponse(body, nil) })
}

// DeleteProductOperation removes a product by ID.
type DeleteProductOperation struct{ client *Client }

// DeleteProduct binds the operation to this client.
func (client *Client) DeleteProduct() DeleteProductOperation { return DeleteProductOperation{client} }

// Definition returns the generated deleteProduct contract.
func (DeleteProductOperation) Definition() sdkgo.MutationDefinition { return DeleteProductDefinition }

// IdempotencyKey derives the stable receipt key from the call ID.
func (DeleteProductOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteProductInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the product and returns its ID.
func (operation DeleteProductOperation) Invoke(call sdkgo.Call, input DeleteProductInput) sdkgo.MutationAttempt[DeletedProduct] {
	if input.ProductID <= 0 {
		return writeBranch("deleteProduct", DeleteProductBranchDefect, DeletedProduct{}, sdkgo.FailureValidation, "product ID must be positive")
	}
	branches := writeBranches{success: DeleteProductBranchDeleted, notFound: DeleteProductBranchNotFound, rejected: DeleteProductBranchProviderRejected, defect: DeleteProductBranchDefect}
	return invokeSafeWrite(call, operation.client, "deleteProduct", http.MethodDelete, productPath(input.ProductID), nil, http.StatusOK, branches, func(_ []byte) (DeletedProduct, error) { return DeletedProduct{ProductID: input.ProductID}, nil })
}

func productPath(productID int64) string {
	return "/products/" + strconv.FormatInt(productID, 10) + ".json"
}
