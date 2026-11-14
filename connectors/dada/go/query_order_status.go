// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dada

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// QueryOrderStatusInput identifies a third-party delivery order.
type QueryOrderStatusInput struct {
	// OrderID is the third-party order_id supplied when creating the delivery order.
	OrderID string `json:"orderID"`
}

// OrderStatus contains the current delivery status without rider personal data.
type OrderStatus struct {
	// OrderID is the third-party order identifier returned by Dada.
	OrderID string `json:"orderID"`
	// StatusCode is Dada's current delivery state code.
	StatusCode int `json:"statusCode"`
	// StatusMessage is Dada's human-readable status label.
	StatusMessage string `json:"statusMessage"`
}

// QueryOrderStatusOperation reads a Dada delivery order's current status.
type QueryOrderStatusOperation struct{ client *Client }

// QueryOrderStatus binds the Query to this client.
func (client *Client) QueryOrderStatus() QueryOrderStatusOperation {
	return QueryOrderStatusOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (QueryOrderStatusOperation) Definition() sdkgo.QueryDefinition {
	return QueryOrderStatusDefinition
}

// Invoke queries Dada and classifies the provider outcome.
func (operation QueryOrderStatusOperation) Invoke(call sdkgo.Call, input QueryOrderStatusInput) sdkgo.QueryAttempt[OrderStatus] {
	response := operation.client.queryOrderStatus(call, input)
	if errors.Is(response.err, errInput) {
		return statusBranch(QueryOrderStatusBranchDefect, sdkgo.FailureValidation, "Dada order ID is invalid")
	}
	if errors.Is(response.err, errCredential) {
		return statusBranch(QueryOrderStatusBranchDefect, sdkgo.FailureAuthentication, "Dada credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[OrderStatus](statusFailure(sdkgo.FailureTransport, "Dada request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[OrderStatus](statusFailure(sdkgo.FailureRateLimit, "Dada rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[OrderStatus](statusFailure(sdkgo.FailureAvailability, "Dada is temporarily unavailable"), response.retryAfter)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return statusBranch(QueryOrderStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Dada response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[OrderStatus](statusFailure(sdkgo.FailureTransport, "Dada response was interrupted"), 0)
	}
	if response.status != http.StatusOK {
		return statusBranch(QueryOrderStatusBranchProviderRejected, sdkgo.FailureProviderRejection, "Dada rejected HTTP "+strconv.Itoa(response.status))
	}
	if !utf8.Valid(response.body) {
		return statusBranch(QueryOrderStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Dada returned invalid UTF-8")
	}
	var wire struct {
		Status string `json:"status"`
		Code   *int   `json:"code"`
		Result *struct {
			OrderID       string `json:"orderId"`
			StatusCode    int    `json:"statusCode"`
			StatusMessage string `json:"statusMsg"`
		} `json:"result"`
	}
	decoder := json.NewDecoder(bytes.NewReader(response.body))
	if decoder.Decode(&wire) != nil {
		return statusBranch(QueryOrderStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Dada returned invalid status JSON")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return statusBranch(QueryOrderStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Dada returned trailing JSON")
	}
	if wire.Status == "fail" && wire.Code != nil {
		return statusBranch(QueryOrderStatusBranchProviderRejected, sdkgo.FailureProviderRejection, "Dada rejected code "+strconv.Itoa(*wire.Code))
	}
	if wire.Status != "success" || wire.Code == nil || *wire.Code != 0 || wire.Result == nil || wire.Result.OrderID != input.OrderID || wire.Result.StatusCode == 0 || wire.Result.StatusMessage == "" {
		return statusBranch(QueryOrderStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Dada returned incomplete or inconsistent order status")
	}
	result := OrderStatus{OrderID: wire.Result.OrderID, StatusCode: wire.Result.StatusCode, StatusMessage: wire.Result.StatusMessage}
	return sdkgo.NewQueryBranch(QueryOrderStatusBranchFound, result, nil, sdkgo.Receipt{})
}

func statusFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "dada", Operation: "queryOrderStatus", Message: message}
}
func statusBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[OrderStatus] {
	failure := statusFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, OrderStatus{}, &failure, sdkgo.Receipt{})
}
