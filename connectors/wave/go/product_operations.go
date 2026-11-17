// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wave

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"regexp"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const listProductsGraphQL = "query ListProducts($businessId: ID!, $page: Int!, $pageSize: Int!) { business(id: $businessId) { products(page: $page, pageSize: $pageSize) { pageInfo { currentPage totalPages totalCount } edges { node { id name description unitPrice isArchived } } } } }"
const getProductGraphQL = "query GetProduct($businessId: ID!, $productId: ID!) { business(id: $businessId) { product(id: $productId) { id name description unitPrice isArchived } } }"
const createProductGraphQL = "mutation CreateProduct($input: ProductCreateInput!) { productCreate(input: $input) { didSucceed inputErrors { code } product { id name description unitPrice isArchived } } }"
const updateProductGraphQL = "mutation UpdateProduct($input: ProductPatchInput!) { productPatch(input: $input) { didSucceed inputErrors { code } product { id name description unitPrice isArchived } } }"
const archiveProductGraphQL = "mutation ArchiveProduct($input: ProductArchiveInput!) { productArchive(input: $input) { didSucceed inputErrors { code } product { id name description unitPrice isArchived } } }"

var pricePattern = regexp.MustCompile(`^[0-9]+(\.[0-9]{1,5})?$`)

// ProductProfile contains public Wave product or service metadata.
type ProductProfile struct {
	// BusinessID identifies the owning business.
	BusinessID string `json:"businessId"`
	// ID is the Wave product ID.
	ID string `json:"id"`
	// Name is the product display name.
	Name string `json:"name"`
	// Description is optional product text.
	Description string `json:"description"`
	// UnitPrice is the decimal price in the business's major currency unit.
	UnitPrice string `json:"unitPrice"`
	// IsArchived reports whether the product is archived.
	IsArchived bool `json:"isArchived"`
}

// ProductPage is one page of Wave products.
type ProductPage struct {
	// BusinessID identifies the queried business.
	BusinessID string `json:"businessId"`
	// Products contains the returned products.
	Products []ProductProfile `json:"products"`
	// CurrentPage is the returned 1-based page.
	CurrentPage int `json:"currentPage"`
	// TotalPages is the number of available pages.
	TotalPages int `json:"totalPages"`
	// TotalCount is the number of matching products.
	TotalCount int `json:"totalCount"`
}

// ListProductsInput selects one page of products in a business.
type ListProductsInput struct {
	// BusinessID is the exact Wave business ID.
	BusinessID string `json:"businessId"`
	// Page is the 1-based page number.
	Page int `json:"page"`
	// PageSize is the number of products, from 1 to 100.
	PageSize int `json:"pageSize"`
}

// GetProductInput selects one product within a business.
type GetProductInput struct {
	// BusinessID is the exact Wave business ID.
	BusinessID string `json:"businessId"`
	// ProductID is the exact Wave product ID.
	ProductID string `json:"productId"`
}

// CreateProductInput defines a sold product and its income account.
type CreateProductInput struct {
	// BusinessID is the owning Wave business ID.
	BusinessID string `json:"businessId"`
	// Name is the new product name.
	Name string `json:"name"`
	// Description is optional product text.
	Description string `json:"description,omitempty"`
	// UnitPrice is a nonnegative decimal amount in the business's major currency unit, with at most five fractional digits.
	UnitPrice string `json:"unitPrice"`
	// IncomeAccountID is a Wave income account ID for a sold product.
	IncomeAccountID string `json:"incomeAccountId"`
}

// UpdateProductInput replaces a product's name and unit price.
type UpdateProductInput struct {
	// BusinessID identifies the business for the returned profile.
	BusinessID string `json:"businessId"`
	// ProductID is the exact Wave product ID.
	ProductID string `json:"productId"`
	// Name is the replacement product name.
	Name string `json:"name"`
	// UnitPrice is the replacement decimal price in the business's major currency unit.
	UnitPrice string `json:"unitPrice"`
}

// ArchiveProductInput identifies a product to archive.
type ArchiveProductInput struct {
	// BusinessID identifies the business for the returned profile.
	BusinessID string `json:"businessId"`
	// ProductID is the exact Wave product ID.
	ProductID string `json:"productId"`
}

// ListProductsOperation reads a page of products.
type ListProductsOperation struct{ client *Client }

// ListProducts binds product enumeration to this client.
func (client *Client) ListProducts() ListProductsOperation { return ListProductsOperation{client} }

// Definition returns the generated Query contract.
func (ListProductsOperation) Definition() sdkgo.QueryDefinition { return ListProductsDefinition }

// Invoke reads one product page.
func (operation ListProductsOperation) Invoke(call sdkgo.Call, input ListProductsInput) sdkgo.QueryAttempt[ProductPage] {
	if !validGraphQLID(input.BusinessID) || input.Page < 1 || input.PageSize < 1 || input.PageSize > 100 {
		return waveQueryBranch[ProductPage]("listProducts", "defect", sdkgo.FailureValidation, "Wave product page input is invalid")
	}
	variables := struct {
		BusinessID string `json:"businessId"`
		Page       int    `json:"page"`
		PageSize   int    `json:"pageSize"`
	}{input.BusinessID, input.Page, input.PageSize}
	response := operation.client.sendGraphQL(call, call.Context, listProductsGraphQL, variables, false)
	if retry, hasRetry := waveQueryRetry[ProductPage]("listProducts", response); hasRetry {
		return retry
	}
	if errors.Is(response.err, errCredential) {
		return waveQueryBranch[ProductPage]("listProducts", "defect", sdkgo.FailureAuthentication, "Wave credentials are invalid")
	}
	if response.status != http.StatusOK {
		return waveQueryBranch[ProductPage]("listProducts", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected product listing")
	}
	if response.err != nil {
		return waveQueryBranch[ProductPage]("listProducts", "invalidResponse", sdkgo.FailureResponseTooLarge, "Wave product response exceeded the limit")
	}
	var wire struct {
		Data *struct {
			Business *struct {
				Products *struct {
					PageInfo struct {
						CurrentPage int `json:"currentPage"`
						TotalPages  int `json:"totalPages"`
						TotalCount  int `json:"totalCount"`
					} `json:"pageInfo"`
					Edges []struct {
						Node ProductProfile `json:"node"`
					} `json:"edges"`
				} `json:"products"`
			} `json:"business"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || len(wire.Errors) > 0 {
		return waveQueryBranch[ProductPage]("listProducts", "invalidResponse", sdkgo.FailureProtocol, "Wave returned invalid product JSON")
	}
	if wire.Data.Business == nil {
		return waveQueryBranch[ProductPage]("listProducts", "notFound", sdkgo.FailureNotFound, "Wave did not find the business")
	}
	if wire.Data.Business.Products == nil || wire.Data.Business.Products.Edges == nil {
		return waveQueryBranch[ProductPage]("listProducts", "invalidResponse", sdkgo.FailureProtocol, "Wave returned incomplete product data")
	}
	page := ProductPage{BusinessID: input.BusinessID, Products: []ProductProfile{}, CurrentPage: wire.Data.Business.Products.PageInfo.CurrentPage, TotalPages: wire.Data.Business.Products.PageInfo.TotalPages, TotalCount: wire.Data.Business.Products.PageInfo.TotalCount}
	for _, edge := range wire.Data.Business.Products.Edges {
		if !validProductProfile(edge.Node) {
			return waveQueryBranch[ProductPage]("listProducts", "invalidResponse", sdkgo.FailureProtocol, "Wave returned incomplete product data")
		}
		edge.Node.BusinessID = input.BusinessID
		page.Products = append(page.Products, edge.Node)
	}
	return sdkgo.NewQueryBranch(sdkgo.BranchID("listed"), page, nil, sdkgo.Receipt{})
}

// GetProductOperation reads one product.
type GetProductOperation struct{ client *Client }

// GetProduct binds a product lookup to this client.
func (client *Client) GetProduct() GetProductOperation { return GetProductOperation{client} }

// Definition returns the generated Query contract.
func (GetProductOperation) Definition() sdkgo.QueryDefinition { return GetProductDefinition }

// Invoke reads one product by business and product IDs.
func (operation GetProductOperation) Invoke(call sdkgo.Call, input GetProductInput) sdkgo.QueryAttempt[ProductProfile] {
	if !validGraphQLID(input.BusinessID) || !validGraphQLID(input.ProductID) {
		return waveQueryBranch[ProductProfile]("getProduct", "defect", sdkgo.FailureValidation, "Wave product IDs are invalid")
	}
	variables := struct {
		BusinessID string `json:"businessId"`
		ProductID  string `json:"productId"`
	}{input.BusinessID, input.ProductID}
	response := operation.client.sendGraphQL(call, call.Context, getProductGraphQL, variables, false)
	if retry, hasRetry := waveQueryRetry[ProductProfile]("getProduct", response); hasRetry {
		return retry
	}
	if errors.Is(response.err, errCredential) {
		return waveQueryBranch[ProductProfile]("getProduct", "defect", sdkgo.FailureAuthentication, "Wave credentials are invalid")
	}
	if response.status != http.StatusOK {
		return waveQueryBranch[ProductProfile]("getProduct", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected product lookup")
	}
	if response.err != nil {
		return waveQueryBranch[ProductProfile]("getProduct", "invalidResponse", sdkgo.FailureResponseTooLarge, "Wave product response exceeded the limit")
	}
	var wire struct {
		Data *struct {
			Business *struct {
				Product *ProductProfile `json:"product"`
			} `json:"business"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Data == nil || len(wire.Errors) > 0 {
		return waveQueryBranch[ProductProfile]("getProduct", "invalidResponse", sdkgo.FailureProtocol, "Wave returned invalid product JSON")
	}
	if wire.Data.Business == nil || wire.Data.Business.Product == nil {
		return waveQueryBranch[ProductProfile]("getProduct", "notFound", sdkgo.FailureNotFound, "Wave did not find the product")
	}
	product := *wire.Data.Business.Product
	if !validProductProfile(product) || product.ID != input.ProductID {
		return waveQueryBranch[ProductProfile]("getProduct", "invalidResponse", sdkgo.FailureProtocol, "Wave returned inconsistent product data")
	}
	product.BusinessID = input.BusinessID
	return sdkgo.NewQueryBranch(sdkgo.BranchID("found"), product, nil, sdkgo.Receipt{})
}

// CreateProductOperation creates a product with bounded possible sends.
type CreateProductOperation struct{ client *Client }

// CreateProduct binds creation to this client.
func (client *Client) CreateProduct() CreateProductOperation { return CreateProductOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateProductOperation) Definition() sdkgo.MutationDefinition { return CreateProductDefinition }

// IdempotencyKey identifies one logical product creation.
func (CreateProductOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateProductInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a product and selects uncertainty when the possible-send cap is reached.
func (operation CreateProductOperation) Invoke(call sdkgo.Call, input CreateProductInput) sdkgo.MutationAttempt[ProductProfile] {
	if !validGraphQLID(input.BusinessID) || !validGraphQLID(input.IncomeAccountID) || !validCustomerName(input.Name) || !validPrice(input.UnitPrice) {
		return waveMutationBranch[ProductProfile]("createProduct", "defect", sdkgo.FailureValidation, "Wave product input is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateProductDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return waveMutationBranch[ProductProfile]("createProduct", "defect", sdkgo.FailureLocalDefect, "Wave dispatch failed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return createProductUncertain()
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	variables := struct {
		Input CreateProductInput `json:"input"`
	}{input}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.sendGraphQL(call, requestContext, createProductGraphQL, variables, true)
		if errors.Is(response.err, errCredential) {
			return nil
		}
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return waveMutationRetry[ProductProfile]("createProduct", sdkgo.FailureAvailability)
	}
	if errors.Is(response.err, errCredential) {
		return createProductUncertain()
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return waveMutationRetry[ProductProfile]("createProduct", sdkgo.FailureAvailability)
		}
		return sdkgo.NewMutationRetry[ProductProfile](waveFailure("createProduct", sdkgo.FailureRateLimit, "Wave rate limited product creation"), response.retryAfter)
	}
	if response.status == 0 || response.status == http.StatusRequestTimeout || response.status >= 500 || err != nil {
		if canResend {
			return waveMutationRetry[ProductProfile]("createProduct", sdkgo.FailureAvailability)
		}
		return createProductUncertain()
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return createProductUncertain()
		}
		return waveMutationBranch[ProductProfile]("createProduct", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected product creation")
	}
	product, didSucceed, isValid := decodeProductMutation(response.body, "productCreate")
	if !isValid {
		return createProductUncertain()
	}
	if !didSucceed {
		if decision.MaximumPriorSends > 0 {
			return createProductUncertain()
		}
		return waveMutationBranch[ProductProfile]("createProduct", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected product creation")
	}
	if !validProductProfile(product) || product.Name != input.Name {
		return createProductUncertain()
	}
	product.BusinessID = input.BusinessID
	return sdkgo.NewMutationBranch(sdkgo.BranchID("created"), product, nil, sdkgo.Receipt{})
}

// UpdateProductOperation replaces a product's name and price.
type UpdateProductOperation struct{ client *Client }

// UpdateProduct binds an absolute update to this client.
func (client *Client) UpdateProduct() UpdateProductOperation { return UpdateProductOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateProductOperation) Definition() sdkgo.MutationDefinition { return UpdateProductDefinition }

// IdempotencyKey identifies retries of one product update.
func (UpdateProductOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateProductInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the product's name and price.
func (operation UpdateProductOperation) Invoke(call sdkgo.Call, input UpdateProductInput) sdkgo.MutationAttempt[ProductProfile] {
	if !validGraphQLID(input.BusinessID) || !validGraphQLID(input.ProductID) || !validCustomerName(input.Name) || !validPrice(input.UnitPrice) {
		return waveMutationBranch[ProductProfile]("updateProduct", "defect", sdkgo.FailureValidation, "Wave product input is invalid")
	}
	variables := struct {
		Input struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			UnitPrice string `json:"unitPrice"`
		} `json:"input"`
	}{}
	variables.Input.ID, variables.Input.Name, variables.Input.UnitPrice = input.ProductID, input.Name, input.UnitPrice
	return executeProductMutation(operation.client, call, "updateProduct", updateProductGraphQL, variables, "productPatch", "updated", input.BusinessID, input.ProductID, input.Name, input.UnitPrice, false)
}

// ArchiveProductOperation archives one product.
type ArchiveProductOperation struct{ client *Client }

// ArchiveProduct binds product archival to this client.
func (client *Client) ArchiveProduct() ArchiveProductOperation {
	return ArchiveProductOperation{client}
}

// Definition returns the generated Mutation contract.
func (ArchiveProductOperation) Definition() sdkgo.MutationDefinition { return ArchiveProductDefinition }

// IdempotencyKey identifies retries of one archive request.
func (ArchiveProductOperation) IdempotencyKey(callID sdkgo.CallID, _ ArchiveProductInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke archives the product by ID.
func (operation ArchiveProductOperation) Invoke(call sdkgo.Call, input ArchiveProductInput) sdkgo.MutationAttempt[ProductProfile] {
	if !validGraphQLID(input.BusinessID) || !validGraphQLID(input.ProductID) {
		return waveMutationBranch[ProductProfile]("archiveProduct", "defect", sdkgo.FailureValidation, "Wave product IDs are invalid")
	}
	variables := struct {
		Input struct {
			ID string `json:"id"`
		} `json:"input"`
	}{}
	variables.Input.ID = input.ProductID
	return executeProductMutation(operation.client, call, "archiveProduct", archiveProductGraphQL, variables, "productArchive", "archived", input.BusinessID, input.ProductID, "", "", true)
}

func executeProductMutation(client *Client, call sdkgo.Call, operationName, query string, variables any, mutationField, branch, businessID, productID, name, price string, isArchive bool) sdkgo.MutationAttempt[ProductProfile] {
	response := client.sendGraphQL(call, call.Context, query, variables, false)
	if retry, hasRetry := waveMutationResponseRetry[ProductProfile](operationName, response); hasRetry {
		return retry
	}
	if errors.Is(response.err, errCredential) {
		return waveMutationBranch[ProductProfile](operationName, "defect", sdkgo.FailureAuthentication, "Wave credentials are invalid")
	}
	if response.status != http.StatusOK {
		return waveMutationBranch[ProductProfile](operationName, "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected product change")
	}
	if response.err != nil {
		return waveMutationBranch[ProductProfile](operationName, "invalidResponse", sdkgo.FailureResponseTooLarge, "Wave product response exceeded the limit")
	}
	product, didSucceed, isValid := decodeProductMutation(response.body, mutationField)
	if !isValid {
		return waveMutationBranch[ProductProfile](operationName, "invalidResponse", sdkgo.FailureProtocol, "Wave returned invalid product JSON")
	}
	if !didSucceed {
		return waveMutationBranch[ProductProfile](operationName, "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected product change")
	}
	if !validProductProfile(product) || product.ID != productID || name != "" && product.Name != name || price != "" && !samePrice(product.UnitPrice, price) || isArchive && !product.IsArchived {
		return waveMutationBranch[ProductProfile](operationName, "invalidResponse", sdkgo.FailureProtocol, "Wave returned inconsistent product data")
	}
	product.BusinessID = businessID
	return sdkgo.NewMutationBranch(sdkgo.BranchID(branch), product, nil, sdkgo.Receipt{})
}

func decodeProductMutation(body []byte, field string) (ProductProfile, bool, bool) {
	var wire struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []json.RawMessage          `json:"errors"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Data == nil || len(wire.Errors) > 0 {
		return ProductProfile{}, false, false
	}
	var result struct {
		DidSucceed bool            `json:"didSucceed"`
		Product    *ProductProfile `json:"product"`
	}
	if json.Unmarshal(wire.Data[field], &result) != nil || result.Product == nil && result.DidSucceed {
		return ProductProfile{}, false, false
	}
	if result.Product == nil {
		return ProductProfile{}, result.DidSucceed, true
	}
	return *result.Product, result.DidSucceed, true
}
func validProductProfile(product ProductProfile) bool {
	return product.ID != "" && product.Name != "" && product.UnitPrice != ""
}
func validPrice(value string) bool { return len(value) <= 32 && pricePattern.MatchString(value) }
func samePrice(actual, requested string) bool {
	actualPrice, actualOK := new(big.Rat).SetString(actual)
	requestedPrice, requestedOK := new(big.Rat).SetString(requested)
	return actualOK && requestedOK && actualPrice.Cmp(requestedPrice) == 0
}
func createProductUncertain() sdkgo.MutationAttempt[ProductProfile] {
	failure := waveFailure("createProduct", sdkgo.FailureTransport, "Wave product may have been created but could not be confirmed")
	return sdkgo.NewMutationUncertain(ProductProfile{}, failure, sdkgo.Receipt{})
}
