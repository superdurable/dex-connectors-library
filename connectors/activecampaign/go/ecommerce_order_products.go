// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListEcommerceOrderProductsInput selects one page of Deep Data order products.
type ListEcommerceOrderProductsInput struct {
	// Offset skips this many order products.
	Offset int `json:"offset,omitempty"`
	// Limit caps the returned products; zero uses the provider default, at most 100.
	Limit int `json:"limit,omitempty"`
}

// GetEcommerceOrderProductInput selects one line item by numeric provider ID.
type GetEcommerceOrderProductInput struct {
	// ProductID is the numeric Deep Data order-product ID.
	ProductID string `json:"productId"`
}

// ListProductsForEcommerceOrderInput selects one order's line items.
type ListProductsForEcommerceOrderInput struct {
	// OrderID is the numeric Deep Data order ID.
	OrderID string `json:"orderId"`
	// Offset skips this many products for the selected order.
	Offset int `json:"offset,omitempty"`
	// Limit caps the returned products; zero uses the provider default, at most 100.
	Limit int `json:"limit,omitempty"`
}

// EcommerceOrderProductMetadata summarizes one order line item without provider links.
type EcommerceOrderProductMetadata struct {
	// ID is the numeric order-product ID.
	ID string `json:"id"`
	// OrderID is the associated Deep Data order ID.
	OrderID string `json:"orderId"`
	// ExternalID is the store product identifier.
	ExternalID string `json:"externalId"`
	// Name is the product display name.
	Name string `json:"name"`
	// PriceMinor is this line's price in minor currency units.
	PriceMinor int64 `json:"priceMinor"`
	// Quantity is the number of units on this line.
	Quantity int64 `json:"quantity"`
}

// EcommerceOrderProductsPage contains one order-product collection.
type EcommerceOrderProductsPage struct {
	// Products contains the selected line items.
	Products []EcommerceOrderProductMetadata `json:"products"`
	// Total is the provider's matching item count, or the returned count if absent.
	Total int `json:"total"`
}

// ListEcommerceOrderProductsOperation reads the global order-product collection.
type ListEcommerceOrderProductsOperation struct{ client *Client }

// GetEcommerceOrderProductOperation reads one order-product line.
type GetEcommerceOrderProductOperation struct{ client *Client }

// ListProductsForEcommerceOrderOperation reads one order's line items.
type ListProductsForEcommerceOrderOperation struct{ client *Client }

// ListEcommerceOrderProducts binds the global listing Query.
func (client *Client) ListEcommerceOrderProducts() ListEcommerceOrderProductsOperation {
	return ListEcommerceOrderProductsOperation{client}
}

// GetEcommerceOrderProduct binds the lookup Query.
func (client *Client) GetEcommerceOrderProduct() GetEcommerceOrderProductOperation {
	return GetEcommerceOrderProductOperation{client}
}

// ListProductsForEcommerceOrder binds the order-specific listing Query.
func (client *Client) ListProductsForEcommerceOrder() ListProductsForEcommerceOrderOperation {
	return ListProductsForEcommerceOrderOperation{client}
}

// Definition returns the manifest-generated global listing contract.
func (ListEcommerceOrderProductsOperation) Definition() sdkgo.QueryDefinition {
	return ListEcommerceOrderProductsDefinition
}

// Definition returns the manifest-generated lookup contract.
func (GetEcommerceOrderProductOperation) Definition() sdkgo.QueryDefinition {
	return GetEcommerceOrderProductDefinition
}

// Definition returns the manifest-generated order listing contract.
func (ListProductsForEcommerceOrderOperation) Definition() sdkgo.QueryDefinition {
	return ListProductsForEcommerceOrderDefinition
}

// Invoke reads all visible Deep Data order products.
func (operation ListEcommerceOrderProductsOperation) Invoke(call sdkgo.Call, input ListEcommerceOrderProductsInput) sdkgo.QueryAttempt[EcommerceOrderProductsPage] {
	branches := queryBranches{found: ListEcommerceOrderProductsBranchListed, rejected: ListEcommerceOrderProductsBranchProviderRejected, invalid: ListEcommerceOrderProductsBranchInvalidResponse, defect: ListEcommerceOrderProductsBranchDefect}
	if input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return queryBranch("listEcommerceOrderProducts", branches.defect, EcommerceOrderProductsPage{}, sdkgo.FailureValidation, "offset or limit is invalid")
	}
	path := "/ecomOrderProducts" + ecommercePageQuery(input.Offset, input.Limit)
	return invokeQuery(call, operation.client, "listEcommerceOrderProducts", path, branches, decodeEcommerceOrderProductsPage)
}

// Invoke reads one Deep Data order-product line by ID.
func (operation GetEcommerceOrderProductOperation) Invoke(call sdkgo.Call, input GetEcommerceOrderProductInput) sdkgo.QueryAttempt[EcommerceOrderProductMetadata] {
	branches := queryBranches{found: GetEcommerceOrderProductBranchFound, notFound: GetEcommerceOrderProductBranchNotFound, rejected: GetEcommerceOrderProductBranchProviderRejected, invalid: GetEcommerceOrderProductBranchInvalidResponse, defect: GetEcommerceOrderProductBranchDefect}
	if !numericID.MatchString(input.ProductID) {
		return queryBranch("getEcommerceOrderProduct", branches.defect, EcommerceOrderProductMetadata{}, sdkgo.FailureValidation, "product ID must be numeric")
	}
	return invokeQuery(call, operation.client, "getEcommerceOrderProduct", "/ecomOrderProducts/"+input.ProductID, branches, decodeEcommerceOrderProduct)
}

// Invoke reads the line items of one Deep Data order.
func (operation ListProductsForEcommerceOrderOperation) Invoke(call sdkgo.Call, input ListProductsForEcommerceOrderInput) sdkgo.QueryAttempt[EcommerceOrderProductsPage] {
	branches := queryBranches{found: ListProductsForEcommerceOrderBranchListed, notFound: ListProductsForEcommerceOrderBranchNotFound, rejected: ListProductsForEcommerceOrderBranchProviderRejected, invalid: ListProductsForEcommerceOrderBranchInvalidResponse, defect: ListProductsForEcommerceOrderBranchDefect}
	if !numericID.MatchString(input.OrderID) || input.Offset < 0 || input.Limit < 0 || input.Limit > 100 {
		return queryBranch("listProductsForEcommerceOrder", branches.defect, EcommerceOrderProductsPage{}, sdkgo.FailureValidation, "order ID must be numeric")
	}
	return invokeQuery(call, operation.client, "listProductsForEcommerceOrder", "/ecomOrders/"+input.OrderID+"/orderProducts"+ecommercePageQuery(input.Offset, input.Limit), branches, decodeEcommerceOrderProductsPage)
}

func ecommercePageQuery(offset, limit int) string {
	query := url.Values{}
	if offset != 0 {
		query.Set("offset", strconv.Itoa(offset))
	}
	if limit != 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if len(query) == 0 {
		return ""
	}
	return "?" + query.Encode()
}

type ecommerceOrderProductWire struct {
	ID         string          `json:"id"`
	OrderID    string          `json:"orderid"`
	ExternalID string          `json:"externalid"`
	Name       string          `json:"name"`
	Price      json.RawMessage `json:"price"`
	Quantity   json.RawMessage `json:"quantity"`
}

func decodeEcommerceOrderProductsPage(body []byte) (EcommerceOrderProductsPage, error) {
	var wire struct {
		EcomOrderProducts []ecommerceOrderProductWire `json:"ecomOrderProducts"`
		Meta              *struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EcommerceOrderProductsPage{}, err
	}
	if wire.EcomOrderProducts == nil {
		return EcommerceOrderProductsPage{}, fmt.Errorf("product collection missing")
	}
	page := EcommerceOrderProductsPage{Products: make([]EcommerceOrderProductMetadata, 0, len(wire.EcomOrderProducts)), Total: len(wire.EcomOrderProducts)}
	if wire.Meta != nil {
		total, err := strconv.Atoi(wire.Meta.Total)
		if err != nil || total < 0 {
			return EcommerceOrderProductsPage{}, fmt.Errorf("product count invalid")
		}
		page.Total = total
	}
	for _, item := range wire.EcomOrderProducts {
		product, err := ecommerceOrderProductMetadata(item)
		if err != nil {
			return EcommerceOrderProductsPage{}, err
		}
		page.Products = append(page.Products, product)
	}
	return page, nil
}

func decodeEcommerceOrderProduct(body []byte) (EcommerceOrderProductMetadata, error) {
	var wire struct {
		EcomOrderProduct *ecommerceOrderProductWire `json:"ecomOrderProduct"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EcommerceOrderProductMetadata{}, err
	}
	if wire.EcomOrderProduct == nil {
		return EcommerceOrderProductMetadata{}, fmt.Errorf("product missing")
	}
	return ecommerceOrderProductMetadata(*wire.EcomOrderProduct)
}

func ecommerceOrderProductMetadata(wire ecommerceOrderProductWire) (EcommerceOrderProductMetadata, error) {
	if !numericID.MatchString(wire.ID) || !numericID.MatchString(wire.OrderID) || wire.ExternalID == "" || wire.Name == "" {
		return EcommerceOrderProductMetadata{}, fmt.Errorf("product identity invalid")
	}
	price, err := parseDealInteger(wire.Price)
	if err != nil {
		return EcommerceOrderProductMetadata{}, fmt.Errorf("product price invalid: %w", err)
	}
	quantity, err := parseDealInteger(wire.Quantity)
	if err != nil || quantity < 0 {
		return EcommerceOrderProductMetadata{}, fmt.Errorf("product quantity invalid")
	}
	return EcommerceOrderProductMetadata{ID: wire.ID, OrderID: wire.OrderID, ExternalID: wire.ExternalID, Name: wire.Name, PriceMinor: price, Quantity: quantity}, nil
}
