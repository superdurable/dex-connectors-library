// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package woocommerce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateOrderOperation creates one order with bounded resend after an unknown outcome.
type CreateOrderOperation struct{ client *Client }

// CreateOrder returns the order creation Mutation bound to this client.
func (client *Client) CreateOrder() CreateOrderOperation { return CreateOrderOperation{client: client} }

// Definition returns the generated createOrder contract.
func (CreateOrderOperation) Definition() sdkgo.MutationDefinition { return CreateOrderDefinition }

// IdempotencyKey records the stable call identity; WooCommerce does not deduplicate it.
func (CreateOrderOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateOrderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an order with a single dispatch per attempt.
func (operation CreateOrderOperation) Invoke(call sdkgo.Call, input CreateOrderInput) sdkgo.MutationAttempt[Order] {
	if input.CustomerID < 0 || len(input.LineItems) == 0 {
		return orderWriteBranch("createOrder", CreateOrderBranchDefect, sdkgo.FailureValidation, "customer and line items are invalid")
	}
	for _, item := range input.LineItems {
		if item.ProductID <= 0 || item.Quantity <= 0 {
			return orderWriteBranch("createOrder", CreateOrderBranchDefect, sdkgo.FailureValidation, "line item product and quantity must be positive")
		}
	}
	payload, err := json.Marshal(struct {
		CustomerID int64           `json:"customer_id,omitempty"`
		LineItems  []OrderLineItem `json:"line_items"`
		Status     string          `json:"status,omitempty"`
	}{CustomerID: input.CustomerID, LineItems: input.LineItems, Status: input.Status})
	if err != nil {
		return orderWriteBranch("createOrder", CreateOrderBranchDefect, sdkgo.FailureValidation, "order could not be encoded")
	}
	return operation.client.writeOrder(call, "createOrder", http.MethodPost, "/orders", payload, true, CreateOrderDefinition.StepDefaults.ExecuteMethodTimeout,
		CreateOrderBranchCreated, "", CreateOrderBranchProviderRejected, CreateOrderBranchInvalidResponse, CreateOrderBranchDefect)
}

// UpdateOrderOperation replaces one order status.
type UpdateOrderOperation struct{ client *Client }

// UpdateOrder returns the order update Mutation bound to this client.
func (client *Client) UpdateOrder() UpdateOrderOperation { return UpdateOrderOperation{client: client} }

// Definition returns the generated updateOrder contract.
func (UpdateOrderOperation) Definition() sdkgo.MutationDefinition { return UpdateOrderDefinition }

// IdempotencyKey records the stable call identity for this absolute replacement.
func (UpdateOrderOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateOrderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the provider order status.
func (operation UpdateOrderOperation) Invoke(call sdkgo.Call, input UpdateOrderInput) sdkgo.MutationAttempt[Order] {
	if input.OrderID <= 0 || input.Status == "" {
		return orderWriteBranch("updateOrder", UpdateOrderBranchDefect, sdkgo.FailureValidation, "orderId and status are required")
	}
	payload, err := json.Marshal(struct {
		Status string `json:"status"`
	}{Status: input.Status})
	if err != nil {
		return orderWriteBranch("updateOrder", UpdateOrderBranchDefect, sdkgo.FailureValidation, "status could not be encoded")
	}
	return operation.client.writeOrder(call, "updateOrder", http.MethodPut, "/orders/"+strconv.FormatInt(input.OrderID, 10), payload, false, 0,
		UpdateOrderBranchUpdated, UpdateOrderBranchNotFound, UpdateOrderBranchProviderRejected, UpdateOrderBranchInvalidResponse, UpdateOrderBranchDefect)
}

// DeleteOrderOperation permanently deletes one order.
type DeleteOrderOperation struct{ client *Client }

// DeleteOrder returns the order delete Mutation bound to this client.
func (client *Client) DeleteOrder() DeleteOrderOperation { return DeleteOrderOperation{client: client} }

// Definition returns the generated deleteOrder contract.
func (DeleteOrderOperation) Definition() sdkgo.MutationDefinition { return DeleteOrderDefinition }

// IdempotencyKey records the stable call identity for this delete.
func (DeleteOrderOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteOrderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke permanently deletes the provider order.
func (operation DeleteOrderOperation) Invoke(call sdkgo.Call, input DeleteOrderInput) sdkgo.MutationAttempt[Order] {
	if input.OrderID <= 0 {
		return orderWriteBranch("deleteOrder", DeleteOrderBranchDefect, sdkgo.FailureValidation, "orderId must be positive")
	}
	return operation.client.writeOrder(call, "deleteOrder", http.MethodDelete, "/orders/"+strconv.FormatInt(input.OrderID, 10)+"?force=true", nil, false, 0,
		DeleteOrderBranchDeleted, DeleteOrderBranchNotFound, DeleteOrderBranchProviderRejected, DeleteOrderBranchInvalidResponse, DeleteOrderBranchDefect)
}

func (client *Client) writeOrder(call sdkgo.Call, operation, method, path string, payload []byte, isLimited bool, timeout time.Duration,
	successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[Order] {
	var fence *singledispatch.Fence
	if isLimited {
		var err error
		fence, err = singledispatch.NewFence(call, timeout, singledispatch.WithMaxSends(call.MaxSends()))
		if err != nil {
			return orderWriteBranch(operation, defectBranch, sdkgo.FailureLocalDefect, "dispatch fence is unavailable")
		}
		decision := fence.Decide()
		if decision.Kind == singledispatch.FailClosed {
			return orderWriteUncertain(operation, sdkgo.FailureTransport, "an earlier create outcome is unknown")
		}
		if decision.Marker.ProviderID != "" {
			return client.readBackOrder(call, operation, decision.Marker.ProviderID, successBranch)
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return orderWriteUncertain(operation, sdkgo.FailureTransport, "the create send cap was reached")
		}
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.ConsumerKey.Reveal()) || !providerhttp.IsHeaderSafeCredential(credentials.ConsumerSecret.Reveal()) {
		if fence != nil && fence.Decide().Kind == singledispatch.SendAllowed {
			if recordErr := fence.RecordNotSent(); recordErr != nil {
				return orderWriteRetry(operation, sdkgo.FailureAvailability, "credential checkpoint is unavailable", 0)
			}
		}
		return orderWriteBranch(operation, defectBranch, sdkgo.FailureAuthentication, "WooCommerce credentials are unavailable")
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return orderWriteBranch(operation, defectBranch, sdkgo.FailureLocalDefect, "provider request could not be built")
	}
	request.SetBasicAuth(credentials.ConsumerKey.Reveal(), credentials.ConsumerSecret.Reveal())
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	var response *http.Response
	if fence != nil {
		if err := singledispatch.PrepareHTTPWrite(request); err != nil {
			return orderWriteBranch(operation, defectBranch, sdkgo.FailureLocalDefect, "provider request is invalid")
		}
		err = fence.Dispatch(func(requestContext context.Context) error {
			response, err = client.httpClient.Do(request.WithContext(requestContext))
			return err
		})
	} else {
		response, err = client.httpClient.Do(request)
	}
	if err != nil {
		if fence != nil && (errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime)) {
			return orderWriteRetry(operation, sdkgo.FailureAvailability, "provider request was not sent", 0)
		}
		if fence != nil && fence.Decide().MaximumPriorSends+1 >= call.MaxSends() {
			return orderWriteUncertain(operation, sdkgo.FailureTransport, "the create send cap was reached after a transport failure")
		}
		return orderWriteRetry(operation, sdkgo.FailureAvailability, "provider write failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if fence != nil {
			if err := fence.RecordCanResend(); err != nil {
				return orderWriteRetry(operation, sdkgo.FailureAvailability, "retry checkpoint is unavailable", 0)
			}
		}
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return orderWriteRetry(operation, kind, "provider asked to retry the write", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		if fence != nil && fence.Decide().MaximumPriorSends+1 >= call.MaxSends() {
			return orderWriteUncertain(operation, sdkgo.FailureAvailability, "the create send cap was reached after an unknown provider response")
		}
		return orderWriteRetry(operation, sdkgo.FailureAvailability, "provider write outcome is unknown", 0)
	}
	if response.StatusCode == http.StatusNotFound && notFoundBranch != "" {
		return orderWriteBranch(operation, notFoundBranch, sdkgo.FailureProviderRejection, "order was not found")
	}
	if response.StatusCode == http.StatusUnauthorized {
		return orderWriteBranch(operation, rejectedBranch, sdkgo.FailureAuthentication, "provider rejected credentials")
	}
	if response.StatusCode == http.StatusForbidden {
		return orderWriteBranch(operation, rejectedBranch, sdkgo.FailureAuthorization, "provider denied access")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return orderWriteBranch(operation, rejectedBranch, sdkgo.FailureProviderRejection, "provider rejected write with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return orderWriteBranch(operation, invalidBranch, sdkgo.FailureResponseTooLarge, "provider response could not be read within the configured limit")
	}
	var order Order
	if err := json.Unmarshal(body, &order); err != nil || order.ID <= 0 {
		return orderWriteBranch(operation, invalidBranch, sdkgo.FailureProtocol, "provider returned invalid order JSON")
	}
	if fence != nil {
		if err := fence.RecordProviderID(strconv.FormatInt(order.ID, 10)); err != nil {
			return orderWriteRetry(operation, sdkgo.FailureAvailability, "created order identity checkpoint is unavailable", 0)
		}
	}
	return sdkgo.NewMutationBranch(successBranch, order, nil, sdkgo.Receipt{})
}

func (client *Client) readBackOrder(call sdkgo.Call, operation, providerID string, successBranch sdkgo.BranchID) sdkgo.MutationAttempt[Order] {
	orderID, err := strconv.ParseInt(providerID, 10, 64)
	if err != nil || orderID <= 0 {
		return orderWriteUncertain(operation, sdkgo.FailureProtocol, "saved order identity is invalid")
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return orderWriteRetry(operation, sdkgo.FailureAvailability, "created order read-back credentials are unavailable", 0)
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+"/orders/"+providerID, nil)
	if err != nil {
		return orderWriteUncertain(operation, sdkgo.FailureLocalDefect, "created order read-back could not be built")
	}
	request.SetBasicAuth(credentials.ConsumerKey.Reveal(), credentials.ConsumerSecret.Reveal())
	response, err := client.httpClient.Do(request)
	if err != nil {
		return orderWriteRetry(operation, sdkgo.FailureAvailability, "created order read-back is unavailable", 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode >= 500 || response.StatusCode == http.StatusTooManyRequests {
		return orderWriteRetry(operation, sdkgo.FailureAvailability, "created order read-back is unavailable", 0)
	}
	if response.StatusCode != http.StatusOK {
		return orderWriteUncertain(operation, sdkgo.FailureProtocol, "created order could not be confirmed")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return orderWriteRetry(operation, sdkgo.FailureAvailability, "created order read-back response is unavailable", 0)
	}
	order, err := decodeGetOrderResponse(body)
	if err != nil || order.ID != orderID {
		return orderWriteUncertain(operation, sdkgo.FailureProtocol, "created order could not be confirmed")
	}
	return sdkgo.NewMutationBranch(successBranch, order, nil, sdkgo.Receipt{})
}

func orderWriteBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Order] {
	return sdkgo.NewMutationBranch(branch, Order{}, failureFor(operation, kind, message), sdkgo.Receipt{})
}

func orderWriteRetry(operation string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[Order] {
	return sdkgo.NewMutationRetry[Order](*failureFor(operation, kind, message), after)
}

func orderWriteUncertain(operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[Order] {
	return sdkgo.NewMutationUncertain(Order{}, *failureFor(operation, kind, message), sdkgo.Receipt{})
}
