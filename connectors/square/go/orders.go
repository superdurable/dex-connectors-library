// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package square

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Order is the connector-safe subset of a Square order.
type Order struct {
	// ID is the Square order ID.
	ID string `json:"id"`
	// LocationID identifies the seller location.
	LocationID string `json:"location_id"`
	// State is the order state, such as OPEN or COMPLETED.
	State string `json:"state"`
	// Version is Square's optimistic concurrency version.
	Version int64 `json:"version"`
	// ReferenceID is the seller's optional external reference.
	ReferenceID string `json:"reference_id,omitempty"`
}

// OrderPage is one page of Square orders.
type OrderPage struct {
	// Orders contains zero or more matching orders.
	Orders []Order `json:"orders"`
	// Cursor continues the search when set.
	Cursor string `json:"cursor"`
}

// SearchOrdersInput searches orders for one seller location.
type SearchOrdersInput struct {
	// LocationID is a Square seller location ID from the Locations API or Dashboard.
	LocationID string `json:"locationId"`
	// Cursor continues a previous page; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// GetOrderInput identifies one Square order.
type GetOrderInput struct {
	// OrderID is the Square order ID.
	OrderID string `json:"orderId"`
}

// CreateOrderInput creates a single custom line item order.
type CreateOrderInput struct {
	// LocationID is the seller location that owns the order.
	LocationID string `json:"locationId"`
	// LineItemName is the custom product or service name.
	LineItemName string `json:"lineItemName"`
	// Quantity is a positive decimal count formatted as a string, such as "1".
	Quantity string `json:"quantity"`
	// BasePriceMoney is the unit price in minor currency units.
	BasePriceMoney Money `json:"basePriceMoney"`
	// ReferenceID is an optional external system reference.
	ReferenceID string `json:"referenceId,omitempty"`
}

// UpdateOrderInput changes the external reference of an open Square order.
type UpdateOrderInput struct {
	// OrderID is the Square order ID.
	OrderID string `json:"orderId"`
	// Version is the current order version from GetOrder or SearchOrders.
	Version int64 `json:"version"`
	// ReferenceID is the new external system reference.
	ReferenceID string `json:"referenceId"`
}

// SearchOrdersOperation searches Square orders.
type SearchOrdersOperation struct{ client *Client }

// GetOrderOperation reads one Square order.
type GetOrderOperation struct{ client *Client }

// CreateOrderOperation creates one Square order.
type CreateOrderOperation struct{ client *Client }

// UpdateOrderOperation updates one Square order.
type UpdateOrderOperation struct{ client *Client }

// SearchOrders binds the searchOrders Query to this client.
func (client *Client) SearchOrders() SearchOrdersOperation { return SearchOrdersOperation{client} }

// GetOrder binds the getOrder Query to this client.
func (client *Client) GetOrder() GetOrderOperation { return GetOrderOperation{client} }

// CreateOrder binds the createOrder Mutation to this client.
func (client *Client) CreateOrder() CreateOrderOperation { return CreateOrderOperation{client} }

// UpdateOrder binds the updateOrder Mutation to this client.
func (client *Client) UpdateOrder() UpdateOrderOperation { return UpdateOrderOperation{client} }

// Definition returns the manifest-generated contract.
func (SearchOrdersOperation) Definition() sdkgo.QueryDefinition { return SearchOrdersDefinition }

// Definition returns the manifest-generated contract.
func (GetOrderOperation) Definition() sdkgo.QueryDefinition { return GetOrderDefinition }

// Definition returns the manifest-generated contract.
func (CreateOrderOperation) Definition() sdkgo.MutationDefinition { return CreateOrderDefinition }

// Definition returns the manifest-generated contract.
func (UpdateOrderOperation) Definition() sdkgo.MutationDefinition { return UpdateOrderDefinition }

// IdempotencyKey derives a stable Square-compatible key for order creation.
func (CreateOrderOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateOrderInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// IdempotencyKey derives a stable Square-compatible key for order updates.
func (UpdateOrderOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateOrderInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// Invoke searches orders at the selected seller location.
func (operation SearchOrdersOperation) Invoke(call sdkgo.Call, input SearchOrdersInput) sdkgo.QueryAttempt[OrderPage] {
	if !validPathSegment(input.LocationID) {
		return failedAttempt[OrderPage]("searchOrders", SearchOrdersBranchDefect, sdkgo.FailureValidation, "locationId is required")
	}
	body := map[string]any{"location_ids": []string{input.LocationID}}
	if input.Cursor != "" {
		body["cursor"] = input.Cursor
	}
	return executeOrderSearch(operation.client, call, body)
}

// Invoke reads a Square order by ID.
func (operation GetOrderOperation) Invoke(call sdkgo.Call, input GetOrderInput) sdkgo.QueryAttempt[Order] {
	if !validPathSegment(input.OrderID) {
		return failedAttempt[Order]("getOrder", GetOrderBranchDefect, sdkgo.FailureValidation, "orderId must be a path segment")
	}
	return executeRead(operation.client, call, "getOrder", "/orders/"+url.PathEscape(input.OrderID), url.Values{}, GetOrderBranchFound, GetOrderBranchNotFound, GetOrderBranchProviderRejected, GetOrderBranchInvalidResponse, GetOrderBranchDefect, decodeOrder)
}

func decodeOrder(body []byte) (Order, error) {
	var response struct {
		Order Order `json:"order"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Order.ID == "" {
		return Order{}, errors.New("missing order")
	}
	return response.Order, nil
}

func decodeOrderPage(body []byte) (OrderPage, error) {
	var page OrderPage
	if err := json.Unmarshal(body, &page); err != nil {
		return OrderPage{}, err
	}
	if page.Orders == nil {
		page.Orders = []Order{}
	}
	return page, nil
}

// Invoke creates an order with one custom-priced line item.
func (operation CreateOrderOperation) Invoke(call sdkgo.Call, input CreateOrderInput) sdkgo.MutationAttempt[Order] {
	quantity, quantityError := strconv.ParseFloat(input.Quantity, 64)
	if !validPathSegment(input.LocationID) || input.LineItemName == "" || quantityError != nil || quantity <= 0 || math.IsInf(quantity, 0) || math.IsNaN(quantity) || input.BasePriceMoney.Amount < 0 || input.BasePriceMoney.Currency == "" {
		return orderMutationDefect("createOrder", CreateOrderBranchDefect)
	}
	order := map[string]any{"location_id": input.LocationID, "line_items": []map[string]any{{"name": input.LineItemName, "quantity": input.Quantity, "base_price_money": input.BasePriceMoney}}}
	if input.ReferenceID != "" {
		order["reference_id"] = input.ReferenceID
	}
	return executeOrderMutation(operation.client, call, orderWriteRequest{"createOrder", http.MethodPost, "/orders", map[string]any{"idempotency_key": call.IdempotencyKey, "order": order}, CreateOrderBranchCreated, "", CreateOrderBranchProviderRejected, CreateOrderBranchInvalidResponse, CreateOrderBranchDefect})
}

// Invoke replaces the external reference on an open order using its current version.
func (operation UpdateOrderOperation) Invoke(call sdkgo.Call, input UpdateOrderInput) sdkgo.MutationAttempt[Order] {
	if !validPathSegment(input.OrderID) || input.Version <= 0 || input.ReferenceID == "" {
		return orderMutationDefect("updateOrder", UpdateOrderBranchDefect)
	}
	order := map[string]any{"version": input.Version, "reference_id": input.ReferenceID}
	return executeOrderMutation(operation.client, call, orderWriteRequest{"updateOrder", http.MethodPut, "/orders/" + url.PathEscape(input.OrderID), map[string]any{"idempotency_key": call.IdempotencyKey, "order": order}, UpdateOrderBranchUpdated, UpdateOrderBranchNotFound, UpdateOrderBranchProviderRejected, UpdateOrderBranchInvalidResponse, UpdateOrderBranchDefect})
}

func executeOrderSearch(client *Client, call sdkgo.Call, body map[string]any) sdkgo.QueryAttempt[OrderPage] {
	payload, err := json.Marshal(body)
	if err != nil {
		return failedAttempt[OrderPage]("searchOrders", SearchOrdersBranchDefect, sdkgo.FailureLocalDefect, "order search input is invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/orders/search", bytes.NewReader(payload))
	if err != nil {
		return failedAttempt[OrderPage]("searchOrders", SearchOrdersBranchDefect, sdkgo.FailureLocalDefect, "order search request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := client.authorize(call, "searchOrders", request, SearchOrdersBranchDefect, SearchOrdersBranchProviderRejected, SearchOrdersBranchInvalidResponse)
	if authorization.failure != nil {
		return sdkgo.NewQueryBranch(authorization.branch, OrderPage{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[OrderPage](*failureFor("searchOrders", sdkgo.FailureAvailability, "provider order search failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[OrderPage](*failureFor("searchOrders", sdkgo.FailureRateLimit, "provider rate limited order search"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewQueryRetry[OrderPage](*failureFor("searchOrders", sdkgo.FailureAvailability, "provider order search is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return failedAttempt[OrderPage]("searchOrders", SearchOrdersBranchProviderRejected, sdkgo.FailureProviderRejection, "provider rejected order search with HTTP "+strconv.Itoa(response.StatusCode))
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedAttempt[OrderPage]("searchOrders", SearchOrdersBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[OrderPage](*failureFor("searchOrders", sdkgo.FailureAvailability, "provider order search response was interrupted"), 0)
	}
	page, err := decodeOrderPage(responseBody)
	if err != nil {
		return failedAttempt[OrderPage]("searchOrders", SearchOrdersBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned invalid JSON")
	}
	return sdkgo.NewQueryBranch(SearchOrdersBranchListed, page, nil, sdkgo.Receipt{})
}

type orderWriteRequest struct {
	operation      string
	method         string
	path           string
	body           map[string]any
	successBranch  sdkgo.BranchID
	notFoundBranch sdkgo.BranchID
	rejectedBranch sdkgo.BranchID
	invalidBranch  sdkgo.BranchID
	defectBranch   sdkgo.BranchID
}

func orderMutationDefect(operation string, branch sdkgo.BranchID) sdkgo.MutationAttempt[Order] {
	return sdkgo.NewMutationBranch(branch, Order{}, failureFor(operation, sdkgo.FailureValidation, "order input is invalid"), sdkgo.Receipt{})
}

func executeOrderMutation(client *Client, call sdkgo.Call, write orderWriteRequest) sdkgo.MutationAttempt[Order] {
	payload, err := json.Marshal(write.body)
	if err != nil {
		return orderMutationDefect(write.operation, write.defectBranch)
	}
	request, err := http.NewRequestWithContext(call.Context, write.method, client.baseURL+write.path, bytes.NewReader(payload))
	if err != nil {
		return orderMutationDefect(write.operation, write.defectBranch)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := client.authorize(call, write.operation, request, write.defectBranch, write.rejectedBranch, write.invalidBranch)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, Order{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[Order](*failureFor(write.operation, sdkgo.FailureAvailability, "provider order write outcome is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[Order](*failureFor(write.operation, sdkgo.FailureRateLimit, "provider rate limited order write"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[Order](*failureFor(write.operation, sdkgo.FailureAvailability, "provider order write is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound && write.notFoundBranch != "":
		return sdkgo.NewMutationBranch(write.notFoundBranch, Order{}, failureFor(write.operation, sdkgo.FailureNotFound, "order was not found"), sdkgo.Receipt{})
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return sdkgo.NewMutationBranch(write.rejectedBranch, Order{}, failureFor(write.operation, sdkgo.FailureProviderRejection, "provider rejected order write with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationBranch(write.invalidBranch, Order{}, failureFor(write.operation, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit"), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[Order](*failureFor(write.operation, sdkgo.FailureAvailability, "provider order response was interrupted"), 0)
	}
	order, err := decodeOrder(responseBody)
	if err != nil {
		return sdkgo.NewMutationBranch(write.invalidBranch, Order{}, failureFor(write.operation, sdkgo.FailureProtocol, "provider returned invalid JSON"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(write.successBranch, order, nil, sdkgo.Receipt{})
}
