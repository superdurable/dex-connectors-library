// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package woocommerce

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateCustomerOperation creates one customer.
type CreateCustomerOperation struct{ client *Client }

// CreateCustomer returns the customer create Mutation.
func (client *Client) CreateCustomer() CreateCustomerOperation {
	return CreateCustomerOperation{client}
}

// Definition returns the generated createCustomer contract.
func (CreateCustomerOperation) Definition() sdkgo.MutationDefinition { return CreateCustomerDefinition }

// IdempotencyKey records the stable call identity for this operation.
func (CreateCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates the provider customer.
func (operation CreateCustomerOperation) Invoke(call sdkgo.Call, input CreateCustomerInput) sdkgo.MutationAttempt[Customer] {
	if !strings.Contains(input.Email, "@") || !validCatalogText(input.Email) {
		return catalogMutationBranch[Customer]("createCustomer", CreateCustomerBranchDefect, sdkgo.FailureValidation, "required customer fields are invalid")
	}
	payload, err := encodeCatalogPayload(map[string]any{"email": input.Email, "first_name": input.FirstName, "last_name": input.LastName})
	if err != nil {
		return catalogMutationBranch[Customer]("createCustomer", CreateCustomerBranchDefect, sdkgo.FailureValidation, "customer could not be encoded")
	}
	return writeCatalogResource(operation.client, call, "createCustomer", http.MethodPost, "/customers", "/customers/", payload, CreateCustomerDefinition.StepDefaults.ExecuteMethodTimeout,
		catalogWriteBranches{success: CreateCustomerBranchCreated, notFound: CreateCustomerBranchNotFound, rejected: CreateCustomerBranchProviderRejected, invalid: CreateCustomerBranchInvalidResponse, defect: CreateCustomerBranchDefect}, decodeCustomer, func(value Customer) int64 { return value.ID })
}

// UpdateCustomerOperation replaces one customer.
type UpdateCustomerOperation struct{ client *Client }

// UpdateCustomer returns the customer update Mutation.
func (client *Client) UpdateCustomer() UpdateCustomerOperation {
	return UpdateCustomerOperation{client}
}

// Definition returns the generated updateCustomer contract.
func (UpdateCustomerOperation) Definition() sdkgo.MutationDefinition { return UpdateCustomerDefinition }

// IdempotencyKey records the stable call identity for this operation.
func (UpdateCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the provider customer.
func (operation UpdateCustomerOperation) Invoke(call sdkgo.Call, input UpdateCustomerInput) sdkgo.MutationAttempt[Customer] {
	if input.CustomerID <= 0 {
		return catalogMutationBranch[Customer]("updateCustomer", UpdateCustomerBranchDefect, sdkgo.FailureValidation, "required customer fields are invalid")
	}
	payload, err := encodeCatalogPayload(map[string]any{"first_name": input.FirstName, "last_name": input.LastName})
	if err != nil {
		return catalogMutationBranch[Customer]("updateCustomer", UpdateCustomerBranchDefect, sdkgo.FailureValidation, "customer could not be encoded")
	}
	return writeCatalogResource(operation.client, call, "updateCustomer", http.MethodPut, "/customers/"+strconv.FormatInt(input.CustomerID, 10), "/customers/", payload, 0,
		catalogWriteBranches{success: UpdateCustomerBranchUpdated, notFound: UpdateCustomerBranchNotFound, rejected: UpdateCustomerBranchProviderRejected, invalid: UpdateCustomerBranchInvalidResponse, defect: UpdateCustomerBranchDefect}, decodeCustomer, func(value Customer) int64 { return value.ID })
}

// DeleteCustomerOperation permanently deletes one customer.
type DeleteCustomerOperation struct{ client *Client }

// DeleteCustomer returns the customer delete Mutation.
func (client *Client) DeleteCustomer() DeleteCustomerOperation {
	return DeleteCustomerOperation{client}
}

// Definition returns the generated deleteCustomer contract.
func (DeleteCustomerOperation) Definition() sdkgo.MutationDefinition { return DeleteCustomerDefinition }

// IdempotencyKey records the stable call identity for this operation.
func (DeleteCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke permanently deletes the provider customer.
func (operation DeleteCustomerOperation) Invoke(call sdkgo.Call, input DeleteCustomerInput) sdkgo.MutationAttempt[Customer] {
	if input.CustomerID <= 0 {
		return catalogMutationBranch[Customer]("deleteCustomer", DeleteCustomerBranchDefect, sdkgo.FailureValidation, "customerId must be positive")
	}
	var payload []byte
	return writeCatalogResource(operation.client, call, "deleteCustomer", http.MethodDelete, "/customers/"+strconv.FormatInt(input.CustomerID, 10)+"?force=true", "/customers/", payload, 0,
		catalogWriteBranches{success: DeleteCustomerBranchDeleted, notFound: DeleteCustomerBranchNotFound, rejected: DeleteCustomerBranchProviderRejected, invalid: DeleteCustomerBranchInvalidResponse, defect: DeleteCustomerBranchDefect}, decodeCustomer, func(value Customer) int64 { return value.ID })
}

// CreateProductOperation creates one product.
type CreateProductOperation struct{ client *Client }

// CreateProduct returns the product create Mutation.
func (client *Client) CreateProduct() CreateProductOperation { return CreateProductOperation{client} }

// Definition returns the generated createProduct contract.
func (CreateProductOperation) Definition() sdkgo.MutationDefinition { return CreateProductDefinition }

// IdempotencyKey records the stable call identity for this operation.
func (CreateProductOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateProductInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates the provider product.
func (operation CreateProductOperation) Invoke(call sdkgo.Call, input CreateProductInput) sdkgo.MutationAttempt[Product] {
	if !validCatalogText(input.Name) || !validCatalogText(input.SKU) || !validCatalogText(input.RegularPrice) {
		return catalogMutationBranch[Product]("createProduct", CreateProductBranchDefect, sdkgo.FailureValidation, "required product fields are invalid")
	}
	payload, err := encodeCatalogPayload(map[string]any{"name": input.Name, "sku": input.SKU, "regular_price": input.RegularPrice, "type": "simple"})
	if err != nil {
		return catalogMutationBranch[Product]("createProduct", CreateProductBranchDefect, sdkgo.FailureValidation, "product could not be encoded")
	}
	return writeCatalogResource(operation.client, call, "createProduct", http.MethodPost, "/products", "/products/", payload, CreateProductDefinition.StepDefaults.ExecuteMethodTimeout,
		catalogWriteBranches{success: CreateProductBranchCreated, notFound: CreateProductBranchNotFound, rejected: CreateProductBranchProviderRejected, invalid: CreateProductBranchInvalidResponse, defect: CreateProductBranchDefect}, decodeProduct, func(value Product) int64 { return value.ID })
}

// UpdateProductOperation replaces one product.
type UpdateProductOperation struct{ client *Client }

// UpdateProduct returns the product update Mutation.
func (client *Client) UpdateProduct() UpdateProductOperation { return UpdateProductOperation{client} }

// Definition returns the generated updateProduct contract.
func (UpdateProductOperation) Definition() sdkgo.MutationDefinition { return UpdateProductDefinition }

// IdempotencyKey records the stable call identity for this operation.
func (UpdateProductOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateProductInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the provider product.
func (operation UpdateProductOperation) Invoke(call sdkgo.Call, input UpdateProductInput) sdkgo.MutationAttempt[Product] {
	if input.ProductID <= 0 || !validCatalogText(input.Name) || !validCatalogText(input.RegularPrice) {
		return catalogMutationBranch[Product]("updateProduct", UpdateProductBranchDefect, sdkgo.FailureValidation, "required product fields are invalid")
	}
	payload, err := encodeCatalogPayload(map[string]any{"name": input.Name, "regular_price": input.RegularPrice})
	if err != nil {
		return catalogMutationBranch[Product]("updateProduct", UpdateProductBranchDefect, sdkgo.FailureValidation, "product could not be encoded")
	}
	return writeCatalogResource(operation.client, call, "updateProduct", http.MethodPut, "/products/"+strconv.FormatInt(input.ProductID, 10), "/products/", payload, 0,
		catalogWriteBranches{success: UpdateProductBranchUpdated, notFound: UpdateProductBranchNotFound, rejected: UpdateProductBranchProviderRejected, invalid: UpdateProductBranchInvalidResponse, defect: UpdateProductBranchDefect}, decodeProduct, func(value Product) int64 { return value.ID })
}

// DeleteProductOperation permanently deletes one product.
type DeleteProductOperation struct{ client *Client }

// DeleteProduct returns the product delete Mutation.
func (client *Client) DeleteProduct() DeleteProductOperation { return DeleteProductOperation{client} }

// Definition returns the generated deleteProduct contract.
func (DeleteProductOperation) Definition() sdkgo.MutationDefinition { return DeleteProductDefinition }

// IdempotencyKey records the stable call identity for this operation.
func (DeleteProductOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteProductInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke permanently deletes the provider product.
func (operation DeleteProductOperation) Invoke(call sdkgo.Call, input DeleteProductInput) sdkgo.MutationAttempt[Product] {
	if input.ProductID <= 0 {
		return catalogMutationBranch[Product]("deleteProduct", DeleteProductBranchDefect, sdkgo.FailureValidation, "productId must be positive")
	}
	var payload []byte
	return writeCatalogResource(operation.client, call, "deleteProduct", http.MethodDelete, "/products/"+strconv.FormatInt(input.ProductID, 10)+"?force=true", "/products/", payload, 0,
		catalogWriteBranches{success: DeleteProductBranchDeleted, notFound: DeleteProductBranchNotFound, rejected: DeleteProductBranchProviderRejected, invalid: DeleteProductBranchInvalidResponse, defect: DeleteProductBranchDefect}, decodeProduct, func(value Product) int64 { return value.ID })
}
