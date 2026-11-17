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
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// OrderIDInput selects a Keap order by ID.
type OrderIDInput struct {
	// OrderID is the ID shown in Keap CRM > Sales > Orders.
	OrderID string `json:"orderId"`
}

// ListOrdersInput selects a bounded page of orders.
type ListOrdersInput struct {
	// PageSize is the maximum orders to return; zero uses 20.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is an opaque continuation token; blank starts the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// CreateOrderInput supplies one product line for a new Keap order.
type CreateOrderInput struct {
	// ContactID is the buyer's Keap contact ID.
	ContactID string `json:"contactId"`
	// ProductID is an existing Keap product ID.
	ProductID string `json:"productId"`
	// Quantity is the number of product units, from 1 to 1000.
	Quantity int `json:"quantity"`
	// Title is the non-empty order title.
	Title string `json:"title"`
	// OrderTime is the order time in RFC 3339 format.
	OrderTime string `json:"orderTime"`
	// OrderType is ONLINE or OFFLINE.
	OrderType string `json:"orderType"`
}

// UpdateOrderTitleInput replaces an order's title.
type UpdateOrderTitleInput struct {
	// OrderID is the existing Keap order ID.
	OrderID string `json:"orderId"`
	// Title is the replacement order title.
	Title string `json:"title"`
}

// OrderProfile contains an order's identity, title, and status.
type OrderProfile struct {
	// ID is Keap's order ID.
	ID string `json:"id"`
	// Title is the order display title.
	Title string `json:"title"`
	// Status is Keap's order status.
	Status string `json:"status"`
}

// OrdersPage contains a page of orders and its continuation token.
type OrdersPage struct {
	// Orders are the returned orders.
	Orders []OrderProfile `json:"orders"`
	// NextPageToken selects the next page, when present.
	NextPageToken string `json:"nextPageToken"`
}

// OrderDeletion confirms deletion of one order.
type OrderDeletion struct {
	// OrderID is the deleted Keap order ID.
	OrderID string `json:"orderId"`
	// Deleted reports Keap's acceptance of the deletion.
	Deleted bool `json:"deleted"`
}

// GetOrderOperation reads one order.
type GetOrderOperation struct{ client *Client }

// ListOrdersOperation reads one page of orders.
type ListOrdersOperation struct{ client *Client }

// CreateOrderOperation creates an order with bounded sends.
type CreateOrderOperation struct{ client *Client }

// UpdateOrderTitleOperation replaces an order's title.
type UpdateOrderTitleOperation struct{ client *Client }

// DeleteOrderOperation deletes an order.
type DeleteOrderOperation struct{ client *Client }

// GetOrder binds an order lookup to the client.
func (client *Client) GetOrder() GetOrderOperation { return GetOrderOperation{client} }

// ListOrders binds an order list to the client.
func (client *Client) ListOrders() ListOrdersOperation { return ListOrdersOperation{client} }

// CreateOrder binds order creation to the client.
func (client *Client) CreateOrder() CreateOrderOperation { return CreateOrderOperation{client} }

// UpdateOrderTitle binds order title replacement to the client.
func (client *Client) UpdateOrderTitle() UpdateOrderTitleOperation {
	return UpdateOrderTitleOperation{client}
}

// DeleteOrder binds order deletion to the client.
func (client *Client) DeleteOrder() DeleteOrderOperation { return DeleteOrderOperation{client} }

// Definition returns the generated query contract.
func (GetOrderOperation) Definition() sdkgo.QueryDefinition { return GetOrderDefinition }

// Definition returns the generated query contract.
func (ListOrdersOperation) Definition() sdkgo.QueryDefinition { return ListOrdersDefinition }

// Definition returns the generated mutation contract.
func (CreateOrderOperation) Definition() sdkgo.MutationDefinition { return CreateOrderDefinition }

// Definition returns the generated mutation contract.
func (UpdateOrderTitleOperation) Definition() sdkgo.MutationDefinition {
	return UpdateOrderTitleDefinition
}

// Definition returns the generated mutation contract.
func (DeleteOrderOperation) Definition() sdkgo.MutationDefinition { return DeleteOrderDefinition }

// IdempotencyKey derives the stable Dex call identity.
func (CreateOrderOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateOrderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable Dex call identity.
func (UpdateOrderTitleOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateOrderTitleInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the stable Dex call identity.
func (DeleteOrderOperation) IdempotencyKey(callID sdkgo.CallID, _ OrderIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads an order by ID.
func (operation GetOrderOperation) Invoke(call sdkgo.Call, input OrderIDInput) sdkgo.QueryAttempt[OrderProfile] {
	if !validContactID(input.OrderID) {
		return keapQueryBranch("getOrder", GetOrderBranchDefect, OrderProfile{}, sdkgo.FailureValidation, "order ID is invalid")
	}
	response := operation.client.request(call, http.MethodGet, "/rest/v2/orders/"+input.OrderID, nil)
	if response.status == 404 {
		return keapQueryBranch("getOrder", GetOrderBranchNotFound, OrderProfile{}, sdkgo.FailureNotFound, "Keap did not find the order")
	}
	if branch, retry, handled := classifyKeapQuery(response, "getOrder", GetOrderBranchProviderRejected, GetOrderBranchInvalidResponse, GetOrderBranchDefect, OrderProfile{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	order, ok := decodeOrder(response.body)
	if !ok {
		return keapQueryBranch("getOrder", GetOrderBranchInvalidResponse, OrderProfile{}, sdkgo.FailureProtocol, "Keap returned invalid order JSON")
	}
	return sdkgo.NewQueryBranch(GetOrderBranchFound, order, nil, sdkgo.Receipt{})
}

// Invoke reads one bounded page of orders.
func (operation ListOrdersOperation) Invoke(call sdkgo.Call, input ListOrdersInput) sdkgo.QueryAttempt[OrdersPage] {
	if input.PageSize < 0 || input.PageSize > 100 || len(input.PageToken) > 2048 {
		return keapQueryBranch("listOrders", ListOrdersBranchDefect, OrdersPage{}, sdkgo.FailureValidation, "page size or token is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}
	query := url.Values{"page_size": {strconv.Itoa(input.PageSize)}}
	if input.PageToken != "" {
		query.Set("page_token", input.PageToken)
	}
	response := operation.client.request(call, http.MethodGet, "/rest/v2/orders?"+query.Encode(), nil)
	if branch, retry, handled := classifyKeapQuery(response, "listOrders", ListOrdersBranchProviderRejected, ListOrdersBranchInvalidResponse, ListOrdersBranchDefect, OrdersPage{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	var wire struct {
		Orders        []json.RawMessage `json:"orders"`
		NextPageToken string            `json:"next_page_token"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Orders == nil {
		return keapQueryBranch("listOrders", ListOrdersBranchInvalidResponse, OrdersPage{}, sdkgo.FailureProtocol, "Keap returned invalid order list JSON")
	}
	page := OrdersPage{Orders: make([]OrderProfile, 0, len(wire.Orders)), NextPageToken: wire.NextPageToken}
	for _, raw := range wire.Orders {
		order, ok := decodeOrder(raw)
		if !ok {
			return keapQueryBranch("listOrders", ListOrdersBranchInvalidResponse, OrdersPage{}, sdkgo.FailureProtocol, "Keap returned invalid order identity")
		}
		page.Orders = append(page.Orders, order)
	}
	return sdkgo.NewQueryBranch(ListOrdersBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke creates an order and one product line, without charging a payment method.
func (operation CreateOrderOperation) Invoke(call sdkgo.Call, input CreateOrderInput) sdkgo.MutationAttempt[OrderProfile] {
	if !validContactID(input.ContactID) || !validContactID(input.ProductID) || input.Quantity < 1 || input.Quantity > 1000 || !validOrderTitle(input.Title) || !validOrderType(input.OrderType) {
		return keapMutationBranch("createOrder", CreateOrderBranchDefect, OrderProfile{}, sdkgo.FailureValidation, "contact, product, quantity, title, or type is invalid")
	}
	if _, err := time.Parse(time.RFC3339, input.OrderTime); err != nil {
		return keapMutationBranch("createOrder", CreateOrderBranchDefect, OrderProfile{}, sdkgo.FailureValidation, "order time must use RFC 3339")
	}
	body, _ := json.Marshal(map[string]any{"contact_id": input.ContactID, "order_title": input.Title, "order_time": input.OrderTime, "order_type": input.OrderType, "order_items": []map[string]any{{"product_id": input.ProductID, "quantity": input.Quantity}}})
	fence, err := singledispatch.NewFence(call, CreateOrderDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return keapMutationBranch("createOrder", CreateOrderBranchDefect, OrderProfile{}, sdkgo.FailureLocalDefect, "order dispatch fence could not be constructed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(OrderProfile{}, keapFailure("createOrder", sdkgo.FailureTransport, "an earlier order create may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.requestWithContext(requestContext, call, http.MethodPost, "/rest/v2/orders", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[OrderProfile](keapFailure("createOrder", sdkgo.FailureAvailability, "order dispatch could not start"), 0)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(OrderProfile{}, keapFailure("createOrder", sdkgo.FailureAuthentication, "earlier order create may have succeeded"), sdkgo.Receipt{})
		}
		return keapMutationBranch("createOrder", CreateOrderBranchDefect, OrderProfile{}, sdkgo.FailureAuthentication, "Keap credentials are unavailable")
	}
	if response.status == 429 || response.status == 503 {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[OrderProfile](keapFailure("createOrder", sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		return sdkgo.NewMutationRetry[OrderProfile](keapFailure("createOrder", sdkgo.FailureAvailability, "Keap did not process the order create"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == 408 || response.status >= 500 || response.err != nil {
		failure := keapFailure("createOrder", sdkgo.FailureTransport, "order create outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[OrderProfile](failure, response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(OrderProfile{}, failure, sdkgo.Receipt{})
	}
	if response.status != 200 && response.status != 201 {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(OrderProfile{}, keapFailure("createOrder", sdkgo.FailureProviderRejection, "earlier order create may have succeeded"), sdkgo.Receipt{})
		}
		return keapMutationBranch("createOrder", CreateOrderBranchProviderRejected, OrderProfile{}, sdkgo.FailureProviderRejection, "Keap rejected order create")
	}
	order, ok := decodeOrder(response.body)
	if !ok {
		return sdkgo.NewMutationUncertain(OrderProfile{}, keapFailure("createOrder", sdkgo.FailureProtocol, "created order identity cannot be confirmed"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateOrderBranchCreated, order, nil, sdkgo.Receipt{})
}

// Invoke replaces an order's title.
func (operation UpdateOrderTitleOperation) Invoke(call sdkgo.Call, input UpdateOrderTitleInput) sdkgo.MutationAttempt[OrderProfile] {
	if !validContactID(input.OrderID) || !validOrderTitle(input.Title) {
		return keapMutationBranch("updateOrderTitle", UpdateOrderTitleBranchDefect, OrderProfile{}, sdkgo.FailureValidation, "order ID and bounded title are required")
	}
	body, _ := json.Marshal(map[string]string{"order_title": input.Title})
	response := operation.client.request(call, http.MethodPatch, "/rest/v2/orders/"+input.OrderID+"?update_mask=order_title", body)
	if response.status == 404 {
		return keapMutationBranch("updateOrderTitle", UpdateOrderTitleBranchNotFound, OrderProfile{}, sdkgo.FailureNotFound, "Keap did not find the order")
	}
	if branch, retry, handled := classifyKeapMutation(response, "updateOrderTitle", UpdateOrderTitleBranchProviderRejected, UpdateOrderTitleBranchInvalidResponse, UpdateOrderTitleBranchDefect, OrderProfile{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	if response.status != 200 {
		return keapMutationBranch("updateOrderTitle", UpdateOrderTitleBranchProviderRejected, OrderProfile{}, sdkgo.FailureProviderRejection, "Keap rejected order update")
	}
	order, ok := decodeOrder(response.body)
	if !ok {
		return keapMutationBranch("updateOrderTitle", UpdateOrderTitleBranchInvalidResponse, OrderProfile{}, sdkgo.FailureProtocol, "Keap returned invalid order JSON")
	}
	return sdkgo.NewMutationBranch(UpdateOrderTitleBranchUpdated, order, nil, sdkgo.Receipt{})
}

// Invoke deletes an order by ID.
func (operation DeleteOrderOperation) Invoke(call sdkgo.Call, input OrderIDInput) sdkgo.MutationAttempt[OrderDeletion] {
	if !validContactID(input.OrderID) {
		return keapMutationBranch("deleteOrder", DeleteOrderBranchDefect, OrderDeletion{}, sdkgo.FailureValidation, "order ID is invalid")
	}
	response := operation.client.request(call, http.MethodDelete, "/rest/v2/orders/"+input.OrderID, nil)
	if response.status == 404 {
		return keapMutationBranch("deleteOrder", DeleteOrderBranchNotFound, OrderDeletion{}, sdkgo.FailureNotFound, "Keap did not find the order")
	}
	if branch, retry, handled := classifyKeapMutation(response, "deleteOrder", DeleteOrderBranchProviderRejected, DeleteOrderBranchInvalidResponse, DeleteOrderBranchDefect, OrderDeletion{}); handled {
		if retry != nil {
			return *retry
		}
		return *branch
	}
	if response.status != 200 && response.status != 204 {
		return keapMutationBranch("deleteOrder", DeleteOrderBranchProviderRejected, OrderDeletion{}, sdkgo.FailureProviderRejection, "Keap rejected order deletion")
	}
	if response.status == 200 && len(response.body) > 0 && !json.Valid(response.body) {
		return keapMutationBranch("deleteOrder", DeleteOrderBranchInvalidResponse, OrderDeletion{}, sdkgo.FailureProtocol, "Keap returned invalid deletion JSON")
	}
	return sdkgo.NewMutationBranch(DeleteOrderBranchDeleted, OrderDeletion{OrderID: input.OrderID, Deleted: true}, nil, sdkgo.Receipt{})
}

func decodeOrder(body []byte) (OrderProfile, bool) {
	var wire struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	}
	if json.Unmarshal(body, &wire) != nil || !validContactID(wire.ID) {
		return OrderProfile{}, false
	}
	return OrderProfile{ID: wire.ID, Title: wire.Title, Status: wire.Status}, true
}

func validOrderTitle(title string) bool { return strings.TrimSpace(title) != "" && len(title) <= 256 }

func validOrderType(orderType string) bool { return orderType == "ONLINE" || orderType == "OFFLINE" }
