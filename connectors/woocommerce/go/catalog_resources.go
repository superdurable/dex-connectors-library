// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package woocommerce

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// Customer is the public, non-secret part of one WooCommerce customer.
type Customer struct {
	// ID is the provider customer ID.
	ID int64 `json:"id"`
	// Email is the customer's email address.
	Email string `json:"email"`
	// FirstName is the customer's first name.
	FirstName string `json:"first_name"`
	// LastName is the customer's last name.
	LastName string `json:"last_name"`
}

// CustomerPage contains one page of customers.
type CustomerPage struct {
	// Customers contains the returned customers, possibly none.
	Customers []Customer `json:"customers"`
}

// ListCustomersInput selects a customer page.
type ListCustomersInput struct {
	// Page is one-based; zero uses the provider default.
	Page int `json:"page,omitempty"`
	// PerPage is at most 100; zero uses the provider default.
	PerPage int `json:"perPage,omitempty"`
}

// GetCustomerInput identifies one customer.
type GetCustomerInput struct {
	// CustomerID is the numeric provider ID.
	CustomerID int64 `json:"customerId"`
}

// CreateCustomerInput describes a new customer.
type CreateCustomerInput struct {
	// Email is the new customer's email address.
	Email string `json:"email"`
	// FirstName is the optional first name.
	FirstName string `json:"firstName,omitempty"`
	// LastName is the optional last name.
	LastName string `json:"lastName,omitempty"`
}

// UpdateCustomerInput replaces one customer's name fields.
type UpdateCustomerInput struct {
	// CustomerID identifies the customer.
	CustomerID int64 `json:"customerId"`
	// FirstName replaces the first name.
	FirstName string `json:"firstName"`
	// LastName replaces the last name.
	LastName string `json:"lastName"`
}

// DeleteCustomerInput identifies one customer to permanently delete.
type DeleteCustomerInput struct {
	// CustomerID identifies the customer.
	CustomerID int64 `json:"customerId"`
}

// Product is a minimal public product summary.
type Product struct {
	// ID is the provider product ID.
	ID int64 `json:"id"`
	// Name is the provider product name.
	Name string `json:"name"`
	// SKU is the merchant stock-keeping unit.
	SKU string `json:"sku"`
	// RegularPrice is a decimal amount in the store currency.
	RegularPrice string `json:"regular_price"`
}

// ProductPage contains one page of products.
type ProductPage struct {
	// Products contains the returned products, possibly none.
	Products []Product `json:"products"`
}

// ListProductsInput selects one product page.
type ListProductsInput struct {
	// Page is one-based; zero uses the provider default.
	Page int `json:"page,omitempty"`
	// PerPage is at most 100; zero uses the provider default.
	PerPage int `json:"perPage,omitempty"`
}

// GetProductInput identifies one product.
type GetProductInput struct {
	// ProductID is the numeric provider ID.
	ProductID int64 `json:"productId"`
}

// CreateProductInput describes a new simple product.
type CreateProductInput struct {
	// Name is the displayed product name.
	Name string `json:"name"`
	// SKU is a merchant-unique stock-keeping unit.
	SKU string `json:"sku"`
	// RegularPrice is a decimal amount in the store currency.
	RegularPrice string `json:"regularPrice"`
}

// UpdateProductInput replaces product display and price fields.
type UpdateProductInput struct {
	// ProductID identifies the product.
	ProductID int64 `json:"productId"`
	// Name is the replacement displayed name.
	Name string `json:"name"`
	// RegularPrice is a decimal amount in the store currency.
	RegularPrice string `json:"regularPrice"`
}

// DeleteProductInput identifies one product to permanently delete.
type DeleteProductInput struct {
	// ProductID identifies the product.
	ProductID int64 `json:"productId"`
}

func decodeResource[T any](body []byte) (T, error) {
	var value T
	if err := json.Unmarshal(body, &value); err != nil {
		return value, err
	}
	return value, nil
}

func resourcePageQuery(page, perPage int) (url.Values, bool) {
	if page < 0 || perPage < 0 || perPage > 100 {
		return nil, false
	}
	query := url.Values{}
	if page > 0 {
		query.Set("page", strconv.Itoa(page))
	}
	if perPage > 0 {
		query.Set("per_page", strconv.Itoa(perPage))
	}
	return query, true
}

func decodeCustomerPage(body []byte) (CustomerPage, error) {
	customers, err := decodeResource[[]Customer](body)
	if err != nil || customers == nil {
		return CustomerPage{}, fmt.Errorf("invalid customer page")
	}
	return CustomerPage{Customers: customers}, nil
}

func decodeProductPage(body []byte) (ProductPage, error) {
	products, err := decodeResource[[]Product](body)
	if err != nil || products == nil {
		return ProductPage{}, fmt.Errorf("invalid product page")
	}
	return ProductPage{Products: products}, nil
}

func decodeCustomer(body []byte) (Customer, error) {
	customer, err := decodeResource[Customer](body)
	if err != nil || customer.ID <= 0 {
		return Customer{}, fmt.Errorf("invalid customer")
	}
	return customer, nil
}

func decodeProduct(body []byte) (Product, error) {
	product, err := decodeResource[Product](body)
	if err != nil || product.ID <= 0 {
		return Product{}, fmt.Errorf("invalid product")
	}
	return product, nil
}

// ListCustomersOperation reads customer pages.
type ListCustomersOperation struct{ client *Client }

// ListCustomers returns the customer list Query.
func (client *Client) ListCustomers() ListCustomersOperation { return ListCustomersOperation{client} }

// Definition returns the generated listCustomers contract.
func (ListCustomersOperation) Definition() sdkgo.QueryDefinition { return ListCustomersDefinition }

// Invoke reads one customer page.
func (operation ListCustomersOperation) Invoke(call sdkgo.Call, input ListCustomersInput) sdkgo.QueryAttempt[CustomerPage] {
	query, valid := resourcePageQuery(input.Page, input.PerPage)
	if !valid {
		return failedAttempt[CustomerPage]("listCustomers", ListCustomersBranchDefect, sdkgo.FailureValidation, "page and perPage must be positive and perPage at most 100")
	}
	return executeRead(operation.client, call, "listCustomers", "/customers", query, ListCustomersBranchListed, ListCustomersBranchNotFound, ListCustomersBranchProviderRejected, ListCustomersBranchInvalidResponse, ListCustomersBranchDefect, decodeCustomerPage)
}

// GetCustomerOperation reads one customer.
type GetCustomerOperation struct{ client *Client }

// GetCustomer returns the customer lookup Query.
func (client *Client) GetCustomer() GetCustomerOperation { return GetCustomerOperation{client} }

// Definition returns the generated getCustomer contract.
func (GetCustomerOperation) Definition() sdkgo.QueryDefinition { return GetCustomerDefinition }

// Invoke reads one customer.
func (operation GetCustomerOperation) Invoke(call sdkgo.Call, input GetCustomerInput) sdkgo.QueryAttempt[Customer] {
	if input.CustomerID <= 0 {
		return failedAttempt[Customer]("getCustomer", GetCustomerBranchDefect, sdkgo.FailureValidation, "customerId must be positive")
	}
	return executeRead(operation.client, call, "getCustomer", "/customers/"+strconv.FormatInt(input.CustomerID, 10), nil, GetCustomerBranchFound, GetCustomerBranchNotFound, GetCustomerBranchProviderRejected, GetCustomerBranchInvalidResponse, GetCustomerBranchDefect, decodeCustomer)
}

// ListProductsOperation reads product pages.
type ListProductsOperation struct{ client *Client }

// ListProducts returns the product list Query.
func (client *Client) ListProducts() ListProductsOperation { return ListProductsOperation{client} }

// Definition returns the generated listProducts contract.
func (ListProductsOperation) Definition() sdkgo.QueryDefinition { return ListProductsDefinition }

// Invoke reads one product page.
func (operation ListProductsOperation) Invoke(call sdkgo.Call, input ListProductsInput) sdkgo.QueryAttempt[ProductPage] {
	query, valid := resourcePageQuery(input.Page, input.PerPage)
	if !valid {
		return failedAttempt[ProductPage]("listProducts", ListProductsBranchDefect, sdkgo.FailureValidation, "page and perPage must be positive and perPage at most 100")
	}
	return executeRead(operation.client, call, "listProducts", "/products", query, ListProductsBranchListed, ListProductsBranchNotFound, ListProductsBranchProviderRejected, ListProductsBranchInvalidResponse, ListProductsBranchDefect, decodeProductPage)
}

// GetProductOperation reads one product.
type GetProductOperation struct{ client *Client }

// GetProduct returns the product lookup Query.
func (client *Client) GetProduct() GetProductOperation { return GetProductOperation{client} }

// Definition returns the generated getProduct contract.
func (GetProductOperation) Definition() sdkgo.QueryDefinition { return GetProductDefinition }

// Invoke reads one product.
func (operation GetProductOperation) Invoke(call sdkgo.Call, input GetProductInput) sdkgo.QueryAttempt[Product] {
	if input.ProductID <= 0 {
		return failedAttempt[Product]("getProduct", GetProductBranchDefect, sdkgo.FailureValidation, "productId must be positive")
	}
	return executeRead(operation.client, call, "getProduct", "/products/"+strconv.FormatInt(input.ProductID, 10), nil, GetProductBranchFound, GetProductBranchNotFound, GetProductBranchProviderRejected, GetProductBranchInvalidResponse, GetProductBranchDefect, decodeProduct)
}
