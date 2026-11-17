// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shopify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// OrderLineItem specifies one product title, quantity, and decimal price.
type OrderLineItem struct {
	// Title is the item label shown on the order.
	Title string `json:"title"`
	// Quantity is a positive number of items.
	Quantity int `json:"quantity"`
	// Price is a nonnegative decimal amount in the shop currency.
	Price string `json:"price"`
}

// CreateOrderInput creates one unpaid order without inventory claims or receipts.
type CreateOrderInput struct {
	// LineItems must contain at least one item.
	LineItems []OrderLineItem `json:"lineItems"`
	// Note is optional non-secret order text.
	Note string `json:"note,omitempty"`
	// Tags is an optional comma-separated tag list.
	Tags string `json:"tags,omitempty"`
}

// UpdateOrderInput replaces an order's note and tags with supplied values.
type UpdateOrderInput struct {
	// OrderID is the numeric Shopify order ID.
	OrderID int64 `json:"orderId"`
	// Note replaces the existing note; blank clears it.
	Note string `json:"note"`
	// Tags replaces the existing comma-separated tags; blank clears them.
	Tags string `json:"tags"`
}

// DeleteOrderInput identifies the order to delete.
type DeleteOrderInput struct {
	// OrderID is the numeric Shopify order ID.
	OrderID int64 `json:"orderId"`
}

// DeletedOrder identifies the removed order.
type DeletedOrder struct {
	// OrderID is the removed Shopify order ID.
	OrderID int64 `json:"orderId"`
}

// CreateOrderOperation creates an order with bounded duplicate sends.
type CreateOrderOperation struct{ client *Client }

// CreateOrder binds the operation to this client.
func (client *Client) CreateOrder() CreateOrderOperation { return CreateOrderOperation{client} }

// Definition returns the generated createOrder contract.
func (CreateOrderOperation) Definition() sdkgo.MutationDefinition { return CreateOrderDefinition }

// IdempotencyKey derives the stable receipt key from the call ID.
func (CreateOrderOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateOrderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one order and bounds ambiguous POST resends.
func (operation CreateOrderOperation) Invoke(call sdkgo.Call, input CreateOrderInput) sdkgo.MutationAttempt[Order] {
	if !isValidCreateOrderInput(input) {
		return writeBranch("createOrder", CreateOrderBranchDefect, Order{}, sdkgo.FailureValidation, "order line items are invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateOrderDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return writeBranch("createOrder", CreateOrderBranchDefect, Order{}, sdkgo.FailureLocalDefect, "order dispatch fence could not be built")
	}
	decision := fence.Decide()
	if decision.Marker.ProviderID != "" {
		providerID, parseError := strconv.ParseInt(decision.Marker.ProviderID, 10, 64)
		if parseError != nil || providerID <= 0 {
			return uncertainOrder(call, "created order checkpoint is invalid")
		}
		response, readError := operation.client.doWrite(call.Context, call, http.MethodGet, "/orders/"+decision.Marker.ProviderID+".json", nil)
		if readError != nil || response.status != http.StatusOK {
			return uncertainOrder(call, "previously created order could not be confirmed")
		}
		order, decodeError := decodeGetOrderResponse(response.body, nil)
		if decodeError != nil || order.ID != providerID {
			return uncertainOrder(call, "previously created order could not be confirmed")
		}
		return sdkgo.NewMutationBranch(CreateOrderBranchCreated, order, nil, writeReceipt(call, decision.Marker.ProviderID))
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return uncertainOrder(call, "an earlier order creation may have succeeded")
	}
	payload, err := json.Marshal(map[string]any{"order": map[string]any{"line_items": input.LineItems, "note": input.Note, "tags": input.Tags, "send_receipt": false, "inventory_behaviour": "bypass"}})
	if err != nil {
		return writeBranch("createOrder", CreateOrderBranchDefect, Order{}, sdkgo.FailureValidation, "order payload could not be encoded")
	}
	var response writeResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		var requestError error
		response, requestError = operation.client.doWrite(requestContext, call, http.MethodPost, "/orders.json", payload)
		return requestError
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[Order](*failureFor("createOrder", sdkgo.FailureAvailability, "order request was not sent"), 0)
	}
	if errors.Is(err, errShopifyCredential) {
		return writeBranch("createOrder", CreateOrderBranchDefect, Order{}, sdkgo.FailureAuthentication, "Shopify credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return uncertainOrder(call, "order retry checkpoint failed")
		}
		return sdkgo.NewMutationRetry[Order](*failureFor("createOrder", sdkgo.FailureAvailability, "order creation was not processed"), response.retryAfter)
	}
	if err != nil || response.status >= 500 || response.status == http.StatusRequestTimeout {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[Order](*failureFor("createOrder", sdkgo.FailureTransport, "order creation outcome is unknown"), 0)
		}
		return uncertainOrder(call, "order creation outcome is unknown after bounded sends")
	}
	if response.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return uncertainOrder(call, "an earlier order creation may have succeeded")
		}
		return writeBranch("createOrder", CreateOrderBranchProviderRejected, Order{}, sdkgo.FailureProviderRejection, "Shopify rejected order creation")
	}
	order, decodeError := decodeGetOrderResponse(response.body, nil)
	if decodeError != nil {
		return uncertainOrder(call, "created order response could not be confirmed")
	}
	if fence.RecordProviderID(strconv.FormatInt(order.ID, 10)) != nil {
		return sdkgo.NewMutationRetry[Order](*failureFor("createOrder", sdkgo.FailureAvailability, "order checkpoint failed"), 0)
	}
	return sdkgo.NewMutationBranch(CreateOrderBranchCreated, order, nil, writeReceipt(call, strconv.FormatInt(order.ID, 10)))
}

func isValidCreateOrderInput(input CreateOrderInput) bool {
	if len(input.LineItems) == 0 || len(input.LineItems) > 250 || len(input.Note) > 5000 || len(input.Tags) > 255 {
		return false
	}
	for _, item := range input.LineItems {
		if strings.TrimSpace(item.Title) == "" || item.Quantity <= 0 || item.Quantity > 100000 || !isValidDecimalPrice(item.Price) {
			return false
		}
	}
	return true
}

func isValidDecimalPrice(value string) bool {
	if value == "" || len(value) > 20 || strings.HasPrefix(value, "-") {
		return false
	}
	parsed, err := strconv.ParseFloat(value, 64)
	return err == nil && parsed >= 0 && !strings.ContainsAny(value, "eE")
}

func uncertainOrder(call sdkgo.Call, message string) sdkgo.MutationAttempt[Order] {
	return sdkgo.NewMutationUncertain(Order{}, *failureFor("createOrder", sdkgo.FailureTransport, message), writeReceipt(call, ""))
}

// UpdateOrderOperation replaces an order's editable note and tags.
type UpdateOrderOperation struct{ client *Client }

// UpdateOrder binds the operation to this client.
func (client *Client) UpdateOrder() UpdateOrderOperation { return UpdateOrderOperation{client} }

// Definition returns the generated updateOrder contract.
func (UpdateOrderOperation) Definition() sdkgo.MutationDefinition { return UpdateOrderDefinition }

// IdempotencyKey derives the stable receipt key from the call ID.
func (UpdateOrderOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateOrderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the same fields on every retry.
func (operation UpdateOrderOperation) Invoke(call sdkgo.Call, input UpdateOrderInput) sdkgo.MutationAttempt[Order] {
	if input.OrderID <= 0 || len(input.Note) > 5000 || len(input.Tags) > 255 {
		return writeBranch("updateOrder", UpdateOrderBranchDefect, Order{}, sdkgo.FailureValidation, "order update input is invalid")
	}
	payload, err := json.Marshal(map[string]any{"order": map[string]any{"id": input.OrderID, "note": input.Note, "tags": input.Tags}})
	if err != nil {
		return writeBranch("updateOrder", UpdateOrderBranchDefect, Order{}, sdkgo.FailureValidation, "order update could not be encoded")
	}
	branches := writeBranches{success: UpdateOrderBranchUpdated, notFound: UpdateOrderBranchNotFound, rejected: UpdateOrderBranchProviderRejected, invalid: UpdateOrderBranchInvalidResponse, defect: UpdateOrderBranchDefect}
	return invokeSafeWrite(call, operation.client, "updateOrder", http.MethodPut, "/orders/"+strconv.FormatInt(input.OrderID, 10)+".json", payload, http.StatusOK, branches, func(body []byte) (Order, error) { return decodeGetOrderResponse(body, nil) })
}

// DeleteOrderOperation deletes an order by ID.
type DeleteOrderOperation struct{ client *Client }

// DeleteOrder binds the operation to this client.
func (client *Client) DeleteOrder() DeleteOrderOperation { return DeleteOrderOperation{client} }

// Definition returns the generated deleteOrder contract.
func (DeleteOrderOperation) Definition() sdkgo.MutationDefinition { return DeleteOrderDefinition }

// IdempotencyKey derives the stable receipt key from the call ID.
func (DeleteOrderOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteOrderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the order and identifies the deleted ID.
func (operation DeleteOrderOperation) Invoke(call sdkgo.Call, input DeleteOrderInput) sdkgo.MutationAttempt[DeletedOrder] {
	if input.OrderID <= 0 {
		return writeBranch("deleteOrder", DeleteOrderBranchDefect, DeletedOrder{}, sdkgo.FailureValidation, "order ID must be positive")
	}
	branches := writeBranches{success: DeleteOrderBranchDeleted, notFound: DeleteOrderBranchNotFound, rejected: DeleteOrderBranchProviderRejected, defect: DeleteOrderBranchDefect}
	return invokeSafeWrite(call, operation.client, "deleteOrder", http.MethodDelete, "/orders/"+strconv.FormatInt(input.OrderID, 10)+".json", nil, http.StatusOK, branches, func(_ []byte) (DeletedOrder, error) { return DeletedOrder{OrderID: input.OrderID}, nil })
}

var errShopifyCredential = errors.New("Shopify credentials are unavailable")

type writeResponse struct {
	body       []byte
	status     int
	retryAfter time.Duration
}
type writeBranches struct{ success, notFound, rejected, invalid, defect sdkgo.BranchID }

func (client *Client) doWrite(requestContext context.Context, call sdkgo.Call, method, path string, payload []byte) (writeResponse, error) {
	request, err := http.NewRequestWithContext(requestContext, method, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return writeResponse{}, err
	}
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	authorization := client.authorize(call, "", request, "", "", "")
	if authorization.failure != nil {
		return writeResponse{}, errShopifyCredential
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return writeResponse{}, err
	}
	defer func() { _ = response.Body.Close() }()
	result := writeResponse{status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())}
	result.body, err = providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return result, err
}

func invokeSafeWrite[T any](call sdkgo.Call, client *Client, operation, method, path string, payload []byte, successStatus int, branches writeBranches, decode func([]byte) (T, error)) sdkgo.MutationAttempt[T] {
	var zero T
	response, err := client.doWrite(call.Context, call, method, path, payload)
	if errors.Is(err, errShopifyCredential) {
		return writeBranch(operation, branches.defect, zero, sdkgo.FailureAuthentication, "Shopify credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[T](*failureFor(operation, sdkgo.FailureRateLimit, "Shopify rate limited the write"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 || err != nil && response.status == 0 {
		return sdkgo.NewMutationRetry[T](*failureFor(operation, sdkgo.FailureAvailability, "Shopify write is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return writeBranch(operation, branches.notFound, zero, sdkgo.FailureNotFound, "resource was not found")
	}
	if response.status != successStatus {
		return writeBranch(operation, branches.rejected, zero, sdkgo.FailureProviderRejection, "Shopify rejected write with HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return writeBranch(operation, branches.invalid, zero, sdkgo.FailureResponseTooLarge, "Shopify response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[T](*failureFor(operation, sdkgo.FailureAvailability, "Shopify response was interrupted"), 0)
	}
	value, err := decode(response.body)
	if err != nil {
		return writeBranch(operation, branches.invalid, zero, sdkgo.FailureProtocol, "Shopify returned invalid JSON")
	}
	return sdkgo.NewMutationBranch(branches.success, value, nil, writeReceipt(call, ""))
}

func writeBranch[T any](operation string, branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationBranch(branch, value, failureFor(operation, kind, message), sdkgo.Receipt{})
}

func writeReceipt(call sdkgo.Call, providerObjectID string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "shopify", ProviderObjectID: providerObjectID}
}
