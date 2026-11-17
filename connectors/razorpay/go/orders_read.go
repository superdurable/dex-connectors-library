// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package razorpay

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetOrderInput selects a Razorpay order by its ID.
type GetOrderInput struct {
	// OrderID is the exact ID prefixed order_.
	OrderID string `json:"orderId"`
}

// ListOrdersInput selects a bounded page of orders.
type ListOrdersInput struct {
	// Count is the page size from 1 to 100.
	Count int `json:"count"`
	// Skip is the non-negative offset into the order collection.
	Skip int `json:"skip"`
}

// OrderPage contains safe order summaries and a page item count.
type OrderPage struct {
	// Orders contains only order identity, state, amount, currency, and receipt.
	Orders []OrderSummary `json:"orders"`
	// Count is the number of returned orders.
	Count int `json:"count"`
}

// GetOrderOperation reads one Razorpay order.
type GetOrderOperation struct{ client *Client }

// GetOrder binds the Query to this client.
func (client *Client) GetOrder() GetOrderOperation { return GetOrderOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetOrderOperation) Definition() sdkgo.QueryDefinition { return GetOrderDefinition }

// Invoke fetches an order and classifies the provider response.
func (operation GetOrderOperation) Invoke(call sdkgo.Call, input GetOrderInput) sdkgo.QueryAttempt[OrderSummary] {
	if !orderIDPattern.MatchString(input.OrderID) {
		return getOrderBranch(GetOrderBranchDefect, sdkgo.FailureValidation, "Razorpay order ID is invalid")
	}
	response := operation.client.get(call, "/orders/"+url.PathEscape(input.OrderID))
	if errors.Is(response.err, errCredential) {
		return getOrderBranch(GetOrderBranchDefect, sdkgo.FailureAuthentication, "Razorpay credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[OrderSummary](orderReadFailure("getOrder", sdkgo.FailureTransport, "Razorpay order request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[OrderSummary](orderReadFailure("getOrder", sdkgo.FailureRateLimit, "Razorpay rate limited order lookup"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[OrderSummary](orderReadFailure("getOrder", sdkgo.FailureAvailability, "Razorpay order lookup is unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return getOrderBranch(GetOrderBranchNotFound, sdkgo.FailureNotFound, "Razorpay order was not found")
	}
	if response.status != http.StatusOK {
		return getOrderBranch(GetOrderBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected order lookup")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return getOrderBranch(GetOrderBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Razorpay order response was oversized")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[OrderSummary](orderReadFailure("getOrder", sdkgo.FailureTransport, "Razorpay order response was interrupted"), 0)
	}
	order, valid := decodeOrderSummary(response.body)
	if !valid || order.ID != input.OrderID {
		return getOrderBranch(GetOrderBranchInvalidResponse, sdkgo.FailureProtocol, "Razorpay order response is invalid")
	}
	return sdkgo.NewQueryBranch(GetOrderBranchFound, order, nil, sdkgo.Receipt{})
}

// ListOrdersOperation reads a bounded page of Razorpay orders.
type ListOrdersOperation struct{ client *Client }

// ListOrders binds the Query to this client.
func (client *Client) ListOrders() ListOrdersOperation { return ListOrdersOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListOrdersOperation) Definition() sdkgo.QueryDefinition { return ListOrdersDefinition }

// Invoke reads one page of orders and strips customer or payment details.
func (operation ListOrdersOperation) Invoke(call sdkgo.Call, input ListOrdersInput) sdkgo.QueryAttempt[OrderPage] {
	if input.Count < 1 || input.Count > 100 || input.Skip < 0 {
		return listOrdersBranch(ListOrdersBranchDefect, sdkgo.FailureValidation, "Razorpay page count or skip is invalid")
	}
	query := url.Values{"count": {strconv.Itoa(input.Count)}, "skip": {strconv.Itoa(input.Skip)}}
	response := operation.client.get(call, "/orders?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return listOrdersBranch(ListOrdersBranchDefect, sdkgo.FailureAuthentication, "Razorpay credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[OrderPage](orderReadFailure("listOrders", sdkgo.FailureTransport, "Razorpay orders request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[OrderPage](orderReadFailure("listOrders", sdkgo.FailureRateLimit, "Razorpay rate limited order listing"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[OrderPage](orderReadFailure("listOrders", sdkgo.FailureAvailability, "Razorpay order listing is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return listOrdersBranch(ListOrdersBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected order listing")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listOrdersBranch(ListOrdersBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Razorpay order list was oversized")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[OrderPage](orderReadFailure("listOrders", sdkgo.FailureTransport, "Razorpay order list was interrupted"), 0)
	}
	var collection struct {
		Entity string            `json:"entity"`
		Count  int               `json:"count"`
		Items  []json.RawMessage `json:"items"`
	}
	if json.Unmarshal(response.body, &collection) != nil || collection.Entity != "collection" || collection.Count < 0 || len(collection.Items) > input.Count {
		return listOrdersBranch(ListOrdersBranchInvalidResponse, sdkgo.FailureProtocol, "Razorpay order list is invalid")
	}
	page := OrderPage{Orders: make([]OrderSummary, 0, len(collection.Items)), Count: collection.Count}
	for _, item := range collection.Items {
		order, valid := decodeOrderSummary(item)
		if !valid {
			return listOrdersBranch(ListOrdersBranchInvalidResponse, sdkgo.FailureProtocol, "Razorpay order item is invalid")
		}
		page.Orders = append(page.Orders, order)
	}
	return sdkgo.NewQueryBranch(ListOrdersBranchListed, page, nil, sdkgo.Receipt{})
}

func orderReadFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "razorpay", Operation: operation, Message: message}
}

func getOrderBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[OrderSummary] {
	failure := orderReadFailure("getOrder", kind, message)
	return sdkgo.NewQueryBranch(branch, OrderSummary{}, &failure, sdkgo.Receipt{})
}

func listOrdersBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[OrderPage] {
	failure := orderReadFailure("listOrders", kind, message)
	return sdkgo.NewQueryBranch(branch, OrderPage{}, &failure, sdkgo.Receipt{})
}
