// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package keap

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

// ProductIDInput selects a Keap product by ID.
type ProductIDInput struct {
	// ProductID is the ID shown in Keap CRM > Products.
	ProductID string `json:"productId"`
}

// ListProductsInput selects a bounded page of products.
type ListProductsInput struct {
	// PageSize is the maximum products to return; zero uses 20.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is an opaque continuation token; blank starts the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// CreateProductInput supplies a new Keap product's catalog details.
type CreateProductInput struct {
	// Name is the non-empty product name, at most 256 bytes.
	Name string `json:"name"`
	// ShortDescription is the required product description, at most 4096 bytes.
	ShortDescription string `json:"shortDescription"`
	// SKU is an optional stock-keeping identifier, at most 128 bytes.
	SKU string `json:"sku,omitempty"`
	// PriceMinor is the price in the smallest currency unit, at least zero.
	PriceMinor int64 `json:"priceMinor"`
}

// UpdateProductInput replaces a product's name and price.
type UpdateProductInput struct {
	// ProductID is the existing Keap product ID.
	ProductID string `json:"productId"`
	// Name is the replacement non-empty name, at most 256 bytes.
	Name string `json:"name"`
	// PriceMinor is the replacement price in the smallest currency unit.
	PriceMinor int64 `json:"priceMinor"`
}

// ProductProfile contains a product's identity, name, SKU, and price.
type ProductProfile struct {
	// ID is Keap's product ID.
	ID string `json:"id"`
	// Name is the product display name.
	Name string `json:"name"`
	// SKU is the optional stock-keeping identifier.
	SKU string `json:"sku"`
	// PriceMinor is the price in the smallest currency unit.
	PriceMinor int64 `json:"priceMinor"`
}

// ProductsPage contains a page of products and its continuation token.
type ProductsPage struct {
	// Products are the returned products.
	Products []ProductProfile `json:"products"`
	// NextPageToken selects the next page, when present.
	NextPageToken string `json:"nextPageToken"`
}

// ProductDeletion confirms deletion of one product.
type ProductDeletion struct {
	// ProductID is the deleted Keap product ID.
	ProductID string `json:"productId"`
	// Deleted reports Keap's acceptance of the deletion.
	Deleted bool `json:"deleted"`
}

// GetProductOperation reads one product.
type GetProductOperation struct{ client *Client }

// ListProductsOperation reads one page of products.
type ListProductsOperation struct{ client *Client }

// CreateProductOperation creates a product with bounded sends.
type CreateProductOperation struct{ client *Client }

// UpdateProductOperation replaces a product's name and price.
type UpdateProductOperation struct{ client *Client }

// DeleteProductOperation deletes a product.
type DeleteProductOperation struct{ client *Client }

// GetProduct binds a product lookup to the client.
func (client *Client) GetProduct() GetProductOperation { return GetProductOperation{client} }

// ListProducts binds a product list to the client.
func (client *Client) ListProducts() ListProductsOperation { return ListProductsOperation{client} }

// CreateProduct binds product creation to the client.
func (client *Client) CreateProduct() CreateProductOperation { return CreateProductOperation{client} }

// UpdateProduct binds product replacement to the client.
func (client *Client) UpdateProduct() UpdateProductOperation {
	return UpdateProductOperation{client}
}

// DeleteProduct binds product deletion to the client.
func (client *Client) DeleteProduct() DeleteProductOperation { return DeleteProductOperation{client} }

// Definition returns the generated query contract.
func (GetProductOperation) Definition() sdkgo.QueryDefinition { return GetProductDefinition }

// Definition returns the generated query contract.
func (ListProductsOperation) Definition() sdkgo.QueryDefinition { return ListProductsDefinition }

// Definition returns the generated mutation contract.
func (CreateProductOperation) Definition() sdkgo.MutationDefinition { return CreateProductDefinition }

// Definition returns the generated mutation contract.
func (UpdateProductOperation) Definition() sdkgo.MutationDefinition {
	return UpdateProductDefinition
}

// Definition returns the generated mutation contract.
func (DeleteProductOperation) Definition() sdkgo.MutationDefinition { return DeleteProductDefinition }

// IdempotencyKey derives the stable Dex call identity.
func (CreateProductOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateProductInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable Dex call identity.
func (UpdateProductOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateProductInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable Dex call identity.
func (DeleteProductOperation) IdempotencyKey(callID sdkgo.CallID, _ ProductIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads an product by ID.
func (operation GetProductOperation) Invoke(call sdkgo.Call, input ProductIDInput) sdkgo.QueryAttempt[ProductProfile] {
	if !validContactID(input.ProductID) {
		return keapQueryBranch("getProduct", GetProductBranchDefect, ProductProfile{}, sdkgo.FailureValidation, "product ID is invalid")
	}
	response := operation.client.request(call, http.MethodGet, "/rest/v2/products/"+input.ProductID, nil)
	if response.status == 404 {
		return keapQueryBranch("getProduct", GetProductBranchNotFound, ProductProfile{}, sdkgo.FailureNotFound, "Keap did not find the product")
	}
	if branch, retry, handled := classifyKeapQuery(response, "getProduct", GetProductBranchProviderRejected, GetProductBranchInvalidResponse, GetProductBranchDefect, ProductProfile{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	product, ok := decodeProduct(response.body)
	if !ok {
		return keapQueryBranch("getProduct", GetProductBranchInvalidResponse, ProductProfile{}, sdkgo.FailureProtocol, "Keap returned invalid product JSON")
	}
	return sdkgo.NewQueryBranch(GetProductBranchFound, product, nil, sdkgo.Receipt{})
}

// Invoke reads one bounded page of products.
func (operation ListProductsOperation) Invoke(call sdkgo.Call, input ListProductsInput) sdkgo.QueryAttempt[ProductsPage] {
	if input.PageSize < 0 || input.PageSize > 100 || len(input.PageToken) > 2048 {
		return keapQueryBranch("listProducts", ListProductsBranchDefect, ProductsPage{}, sdkgo.FailureValidation, "page size or token is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	query := url.Values{"page_size": {strconv.Itoa(input.PageSize)}}
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	response := operation.client.request(call, http.MethodGet, "/rest/v2/products?"+query.Encode(), nil)
	if branch, retry, handled := classifyKeapQuery(response, "listProducts", ListProductsBranchProviderRejected, ListProductsBranchInvalidResponse, ListProductsBranchDefect, ProductsPage{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	var wire struct {
		Products      []json.RawMessage `json:"products"`
		NextPageToken string            `json:"next_page_token"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Products == nil {
		return keapQueryBranch("listProducts", ListProductsBranchInvalidResponse, ProductsPage{}, sdkgo.FailureProtocol, "Keap returned invalid product list JSON")
	}
	page := ProductsPage{Products: make([]ProductProfile, 0, len(wire.Products)), NextPageToken: wire.NextPageToken}
	for _, raw := range wire.Products {
		product, ok := decodeProduct(raw)
		if !ok {
			return keapQueryBranch("listProducts", ListProductsBranchInvalidResponse, ProductsPage{}, sdkgo.FailureProtocol, "Keap returned invalid product identity")
		}
		page.Products = append(page.Products, product)
	}
	return sdkgo.NewQueryBranch(ListProductsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke creates a catalog product with bounded sends.
func (operation CreateProductOperation) Invoke(call sdkgo.Call, input CreateProductInput) sdkgo.MutationAttempt[ProductProfile] {
	if !validProductName(input.Name) || strings.TrimSpace(input.ShortDescription) == "" || len(input.ShortDescription) > 4096 || len(input.SKU) > 128 || input.PriceMinor < 0 {
		return keapMutationBranch("createProduct", CreateProductBranchDefect, ProductProfile{}, sdkgo.FailureValidation, "name, description, SKU, or price is invalid")
	}
	body, _ := json.Marshal(map[string]any{"name": input.Name, "short_description": input.ShortDescription, "sku": input.SKU, "price": input.PriceMinor})
	fence, err := singledispatch.NewFence(call, CreateProductDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return keapMutationBranch("createProduct", CreateProductBranchDefect, ProductProfile{}, sdkgo.FailureLocalDefect, "product dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(ProductProfile{}, keapFailure("createProduct", sdkgo.FailureTransport, "an earlier product create may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.requestWithContext(requestContext, call, http.MethodPost, "/rest/v2/products", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[ProductProfile](keapFailure("createProduct", sdkgo.FailureAvailability, "product dispatch could not start"), 0)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(ProductProfile{}, keapFailure("createProduct", sdkgo.FailureAuthentication, "earlier product create may have succeeded"), sdkgo.Receipt{})
		}
		return keapMutationBranch("createProduct", CreateProductBranchDefect, ProductProfile{}, sdkgo.FailureAuthentication, "Keap credentials are unavailable")
	}
	if response.status == 429 || response.status == 503 {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[ProductProfile](keapFailure("createProduct", sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		return sdkgo.NewMutationRetry[ProductProfile](keapFailure("createProduct", sdkgo.FailureAvailability, "Keap did not process the product create"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == 408 || response.status >= 500 || response.err != nil {
		failure := keapFailure("createProduct", sdkgo.FailureTransport, "product create outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[ProductProfile](failure, response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(ProductProfile{}, failure, sdkgo.Receipt{})
	}
	if response.status != 200 && response.status != 201 {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(ProductProfile{}, keapFailure("createProduct", sdkgo.FailureProviderRejection, "earlier product create may have succeeded"), sdkgo.Receipt{})
		}
		return keapMutationBranch("createProduct", CreateProductBranchProviderRejected, ProductProfile{}, sdkgo.FailureProviderRejection, "Keap rejected product create")
	}
	product, ok := decodeProduct(response.body)
	if !ok {
		return sdkgo.NewMutationUncertain(ProductProfile{}, keapFailure("createProduct", sdkgo.FailureProtocol, "created product identity cannot be confirmed"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateProductBranchCreated, product, nil, sdkgo.Receipt{})
}

// Invoke replaces a product's name and price.
func (operation UpdateProductOperation) Invoke(call sdkgo.Call, input UpdateProductInput) sdkgo.MutationAttempt[ProductProfile] {
	if !validContactID(input.ProductID) || !validProductName(input.Name) || input.PriceMinor < 0 {
		return keapMutationBranch("updateProduct", UpdateProductBranchDefect, ProductProfile{}, sdkgo.FailureValidation, "product ID, name, or price is invalid")
	}
	body, _ := json.Marshal(map[string]any{"name": input.Name, "price": input.PriceMinor})
	response := operation.client.request(call, http.MethodPatch, "/rest/v2/products/"+input.ProductID+"?update_mask=name,price", body)
	if response.status == 404 {
		return keapMutationBranch("updateProduct", UpdateProductBranchNotFound, ProductProfile{}, sdkgo.FailureNotFound, "Keap did not find the product")
	}
	if branch, retry, handled := classifyKeapMutation(response, "updateProduct", UpdateProductBranchProviderRejected, UpdateProductBranchInvalidResponse, UpdateProductBranchDefect, ProductProfile{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	if response.status != 200 {
		return keapMutationBranch("updateProduct", UpdateProductBranchProviderRejected, ProductProfile{}, sdkgo.FailureProviderRejection, "Keap rejected product update")
	}
	product, ok := decodeProduct(response.body)
	if !ok {
		return keapMutationBranch("updateProduct", UpdateProductBranchInvalidResponse, ProductProfile{}, sdkgo.FailureProtocol, "Keap returned invalid product JSON")
	}
	return sdkgo.NewMutationBranch(UpdateProductBranchUpdated, product, nil, sdkgo.Receipt{})
}

// Invoke deletes an product by ID.
func (operation DeleteProductOperation) Invoke(call sdkgo.Call, input ProductIDInput) sdkgo.MutationAttempt[ProductDeletion] {
	if !validContactID(input.ProductID) {
		return keapMutationBranch("deleteProduct", DeleteProductBranchDefect, ProductDeletion{}, sdkgo.FailureValidation, "product ID is invalid")
	}
	response := operation.client.request(call, http.MethodDelete, "/rest/v2/products/"+input.ProductID, nil)
	if response.status == 404 {
		return keapMutationBranch("deleteProduct", DeleteProductBranchNotFound, ProductDeletion{}, sdkgo.FailureNotFound, "Keap did not find the product")
	}
	if branch, retry, handled := classifyKeapMutation(response, "deleteProduct", DeleteProductBranchProviderRejected, DeleteProductBranchInvalidResponse, DeleteProductBranchDefect, ProductDeletion{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	if response.status != 200 && response.status != 204 {
		return keapMutationBranch("deleteProduct", DeleteProductBranchProviderRejected, ProductDeletion{}, sdkgo.FailureProviderRejection, "Keap rejected product deletion")
	}
	if response.status == 200 && len(response.body) > 0 && !json.Valid(response.body) {
		return keapMutationBranch("deleteProduct", DeleteProductBranchInvalidResponse, ProductDeletion{}, sdkgo.FailureProtocol, "Keap returned invalid deletion JSON")
	}
	return sdkgo.NewMutationBranch(DeleteProductBranchDeleted, ProductDeletion{ProductID: input.ProductID, Deleted: true}, nil, sdkgo.Receipt{})
}

func decodeProduct(body []byte) (ProductProfile, bool) {
	var wire struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		SKU   string `json:"sku"`
		Price *struct {
			Amount int64 `json:"amount"`
		} `json:"price"`
	}
	if json.Unmarshal(body, &wire) != nil || !validContactID(wire.ID) || !validProductName(wire.Name) || wire.Price == nil || wire.Price.Amount < 0 {
		return ProductProfile{}, false
	}
	return ProductProfile{ID: wire.ID, Name: wire.Name, SKU: wire.SKU, PriceMinor: wire.Price.Amount}, true
}

func validProductName(name string) bool { return strings.TrimSpace(name) != "" && len(name) <= 256 }
