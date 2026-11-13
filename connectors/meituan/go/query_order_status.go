// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meituan

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// QueryOrderStatusInput identifies one order from the last 90 days.
type QueryOrderStatusInput struct {
	// DeliveryID is the positive delivery activity ID used when the order was created.
	DeliveryID int64 `json:"deliveryId"`
	// MeituanOrderID is the provider's order ID, at most 32 characters.
	MeituanOrderID string `json:"meituanOrderId"`
}

// OrderStatusResult contains only the delivery status and associated identifiers.
type OrderStatusResult struct {
	// DeliveryID is the delivery activity ID.
	DeliveryID int64 `json:"deliveryId"`
	// MeituanOrderID is the provider's order ID.
	MeituanOrderID string `json:"meituanOrderId"`
	// MerchantOrderID is the merchant's order ID.
	MerchantOrderID string `json:"merchantOrderId"`
	// Status is 0 pending, 20 accepted, 30 picked up, 50 delivered, or 99 canceled.
	Status int `json:"status"`
	// OperatedAt is the provider's Unix timestamp for the latest status change.
	OperatedAt int64 `json:"operatedAt"`
}

// QueryOrderStatusOperation reads the current status for one delivery order.
type QueryOrderStatusOperation struct{ client *Client }

// Definition returns the generated order-status Query contract.
func (QueryOrderStatusOperation) Definition() sdkgo.QueryDefinition {
	return QueryOrderStatusDefinition
}

// Invoke signs and sends one query, then classifies provider and transport outcomes.
func (operation QueryOrderStatusOperation) Invoke(call sdkgo.Call, input QueryOrderStatusInput) sdkgo.QueryAttempt[OrderStatusResult] {
	if input.DeliveryID <= 0 || !validOrderID(input.MeituanOrderID) {
		return orderBranch(QueryOrderStatusBranchDefect, sdkgo.FailureValidation, "Meituan delivery order identifiers are invalid")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validCredential(credentials.AppKey) || !validCredential(credentials.Secret.Reveal()) {
		return orderBranch(QueryOrderStatusBranchDefect, sdkgo.FailureAuthentication, "Meituan Delivery credentials are unavailable or invalid")
	}
	form := url.Values{
		"appkey":        {credentials.AppKey},
		"timestamp":     {strconv.FormatInt(time.Now().Unix(), 10)},
		"version":       {"1.0"},
		"delivery_id":   {strconv.FormatInt(input.DeliveryID, 10)},
		"mt_peisong_id": {input.MeituanOrderID},
	}
	form.Set("sign", signOrderStatus(form, credentials.Secret.Reveal()))
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/api/order/status/query", strings.NewReader(form.Encode()))
	if err != nil {
		return orderBranch(QueryOrderStatusBranchDefect, sdkgo.FailureValidation, "Meituan Delivery order request is invalid")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return orderRetry(sdkgo.FailureAvailability, "Meituan Delivery order request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return orderRetry(sdkgo.FailureRateLimit, "Meituan Delivery rate limited order query", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return orderRetry(sdkgo.FailureAvailability, "Meituan Delivery order API is temporarily unavailable", retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return orderBranch(QueryOrderStatusBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Meituan Delivery response exceeded the configured limit")
	}
	if err != nil {
		return orderRetry(sdkgo.FailureAvailability, "Meituan Delivery order response was interrupted", 0)
	}
	if response.StatusCode != http.StatusOK {
		return orderBranch(QueryOrderStatusBranchProviderRejected, classifyOrderHTTP(response.StatusCode), "Meituan Delivery rejected order query with HTTP "+strconv.Itoa(response.StatusCode))
	}
	result, code, err := decodeOrderStatus(body, input)
	if err != nil {
		return orderBranch(QueryOrderStatusBranchInvalidResponse, sdkgo.FailureProtocol, "Meituan Delivery returned an invalid order response")
	}
	if code == 11 {
		return orderRetry(sdkgo.FailureRateLimit, "Meituan Delivery rate limited order query", retryAfter)
	}
	if code != 0 {
		return orderBranch(QueryOrderStatusBranchProviderRejected, sdkgo.FailureProviderRejection, "Meituan Delivery rejected order query")
	}
	return sdkgo.NewQueryBranch(QueryOrderStatusBranchFound, result, nil, sdkgo.Receipt{})
}

func decodeOrderStatus(body []byte, input QueryOrderStatusInput) (OrderStatusResult, int, error) {
	var envelope struct {
		Code *int `json:"code"`
		Data *struct {
			DeliveryID      int64  `json:"delivery_id"`
			MeituanOrderID  string `json:"mt_peisong_id"`
			MerchantOrderID string `json:"order_id"`
			Status          *int   `json:"status"`
			OperatedAt      int64  `json:"operate_time"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Code == nil {
		return OrderStatusResult{}, 0, errors.New("invalid Meituan response")
	}
	if *envelope.Code != 0 {
		return OrderStatusResult{}, *envelope.Code, nil
	}
	if envelope.Data == nil || envelope.Data.DeliveryID != input.DeliveryID || envelope.Data.MeituanOrderID != input.MeituanOrderID || !validOrderID(envelope.Data.MerchantOrderID) || envelope.Data.Status == nil || envelope.Data.OperatedAt <= 0 {
		return OrderStatusResult{}, 0, errors.New("invalid Meituan order status")
	}
	switch *envelope.Data.Status {
	case 0, 20, 30, 50, 99:
	default:
		return OrderStatusResult{}, 0, errors.New("unknown Meituan order status")
	}
	return OrderStatusResult{DeliveryID: envelope.Data.DeliveryID, MeituanOrderID: envelope.Data.MeituanOrderID, MerchantOrderID: envelope.Data.MerchantOrderID, Status: *envelope.Data.Status, OperatedAt: envelope.Data.OperatedAt}, 0, nil
}

func signOrderStatus(form url.Values, secret string) string {
	keys := make([]string, 0, len(form))
	for key := range form {
		if key != "sign" && form.Get(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	var payload strings.Builder
	payload.WriteString(secret)
	for _, key := range keys {
		payload.WriteString(key)
		payload.WriteString(form.Get(key))
	}
	digest := sha1.Sum([]byte(payload.String()))
	return hex.EncodeToString(digest[:])
}
func validOrderID(value string) bool {
	if len(value) == 0 || len(value) > 32 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}
func validCredential(value string) bool {
	return value != "" && len(value) <= 2048 && strings.TrimSpace(value) == value
}
func classifyOrderHTTP(status int) sdkgo.FailureKind {
	switch status {
	case 401:
		return sdkgo.FailureAuthentication
	case 403:
		return sdkgo.FailureAuthorization
	case 400, 404, 412:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}
func orderBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[OrderStatusResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "meituan", Operation: "queryOrderStatus", Message: message}
	return sdkgo.NewQueryBranch(branch, OrderStatusResult{}, &failure, sdkgo.Receipt{})
}
func orderRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[OrderStatusResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "meituan", Operation: "queryOrderStatus", Message: message}
	return sdkgo.NewQueryRetry[OrderStatusResult](failure, delay)
}
