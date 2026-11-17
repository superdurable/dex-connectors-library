// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package razorpay

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CreateOrderInput specifies an order amount and ISO 4217 currency.
type CreateOrderInput struct {
	// Amount is the positive order amount in the currency's smallest subunit.
	Amount int64 `json:"amount"`
	// CurrencyCode is a three-letter uppercase ISO 4217 code.
	CurrencyCode string `json:"currencyCode"`
}

// OrderSummary contains safe order metadata without customer information.
type OrderSummary struct {
	// ID is the Razorpay order ID.
	ID string `json:"id"`
	// Status is the current Razorpay order state.
	Status string `json:"status"`
	// Amount is the order amount in currency subunits.
	Amount int64 `json:"amount"`
	// CurrencyCode is the ISO 4217 code.
	CurrencyCode string `json:"currencyCode"`
	// Receipt is the request-specific identifier generated from the Dex Call ID.
	Receipt string `json:"receipt"`
}

// CreateOrderOperation creates an order with a unique, repeat-safe receipt.
type CreateOrderOperation struct{ client *Client }

// CreateOrder binds the order Mutation to this client.
func (client *Client) CreateOrder() CreateOrderOperation { return CreateOrderOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (CreateOrderOperation) Definition() sdkgo.MutationDefinition { return CreateOrderDefinition }

// IdempotencyKey uses the stable Dex Call ID to derive Razorpay's unique receipt.
func (CreateOrderOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateOrderInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates or confirms an order, using the provider receipt as its duplicate guard.
func (operation CreateOrderOperation) Invoke(call sdkgo.Call, input CreateOrderInput) sdkgo.MutationAttempt[OrderSummary] {
	if input.Amount <= 0 || !validCurrencyCode(input.CurrencyCode) {
		return orderBranch(CreateOrderBranchDefect, sdkgo.FailureValidation, "Razorpay order amount or currency is invalid")
	}
	receipt := "dex_" + strings.ReplaceAll(string(call.IdempotencyKey), "-", "")
	if len(receipt) != 36 {
		return orderBranch(CreateOrderBranchDefect, sdkgo.FailureLocalDefect, "Razorpay order receipt could not be derived")
	}
	payload, _ := json.Marshal(map[string]any{"amount": input.Amount, "currency": input.CurrencyCode, "receipt": receipt})
	response := operation.client.postOrder(call, payload)
	if errors.Is(response.err, errCredential) {
		return orderBranch(CreateOrderBranchDefect, sdkgo.FailureAuthentication, "Razorpay credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return orderRetry(sdkgo.FailureTransport, "Razorpay order request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return orderRetry(sdkgo.FailureRateLimit, "Razorpay rate limited order creation", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return orderRetry(sdkgo.FailureAvailability, "Razorpay order outcome is unknown", response.retryAfter)
	}
	if response.status == http.StatusBadRequest && response.err == nil && isDuplicateOrderReceipt(response.body) {
		return operation.confirmOrder(call, input, receipt)
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		return orderBranch(CreateOrderBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected order creation")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return orderRetry(sdkgo.FailureResponseTooLarge, "Razorpay order may exist but response was oversized", 0)
	}
	if response.err != nil {
		return orderRetry(sdkgo.FailureTransport, "Razorpay order response was interrupted", 0)
	}
	order, valid := decodeOrderSummary(response.body)
	if !valid || order.Receipt != receipt || order.Amount != input.Amount || order.CurrencyCode != input.CurrencyCode {
		return orderRetry(sdkgo.FailureProtocol, "Razorpay order may exist but response was invalid", 0)
	}
	return sdkgo.NewMutationBranch(CreateOrderBranchCreated, order, nil, sdkgo.Receipt{})
}

func (operation CreateOrderOperation) confirmOrder(call sdkgo.Call, input CreateOrderInput, receipt string) sdkgo.MutationAttempt[OrderSummary] {
	response := operation.client.get(call, "/orders?receipt="+url.QueryEscape(receipt)+"&count=100")
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return orderRetry(sdkgo.FailureAvailability, "Razorpay order receipt could not be checked", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return orderBranch(CreateOrderBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected receipt lookup")
	}
	var collection struct {
		Entity string            `json:"entity"`
		Items  []json.RawMessage `json:"items"`
	}
	if json.Unmarshal(response.body, &collection) != nil || collection.Entity != "collection" {
		return orderRetry(sdkgo.FailureProtocol, "Razorpay receipt lookup is invalid", 0)
	}
	for _, item := range collection.Items {
		order, valid := decodeOrderSummary(item)
		if valid && order.Receipt == receipt && order.Amount == input.Amount && order.CurrencyCode == input.CurrencyCode {
			return sdkgo.NewMutationBranch(CreateOrderBranchCreated, order, nil, sdkgo.Receipt{})
		}
	}
	return orderRetry(sdkgo.FailureAvailability, "Razorpay order receipt is not yet visible", 0)
}

func (client *Client) postOrder(call sdkgo.Call, payload []byte) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	keyID, keySecret := credentials.KeyID.Reveal(), credentials.KeySecret.Reveal()
	if !providerhttp.IsHeaderSafeCredential(keyID) || strings.Contains(keyID, ":") || !providerhttp.IsHeaderSafeCredential(keySecret) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/orders", bytes.NewReader(payload))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(keyID+":"+keySecret)))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}

var orderIDPattern = regexp.MustCompile(`^order_[A-Za-z0-9]{6,64}$`)
var currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

func validCurrencyCode(currencyCode string) bool {
	return currencyCodePattern.MatchString(currencyCode)
}

func decodeOrderSummary(body []byte) (OrderSummary, bool) {
	var wire struct {
		ID       string `json:"id"`
		Entity   string `json:"entity"`
		Status   string `json:"status"`
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
		Receipt  string `json:"receipt"`
	}
	if json.Unmarshal(body, &wire) != nil || !orderIDPattern.MatchString(wire.ID) || wire.Entity != "order" || wire.Status == "" || wire.Amount <= 0 || !validCurrencyCode(wire.Currency) {
		return OrderSummary{}, false
	}
	return OrderSummary{ID: wire.ID, Status: wire.Status, Amount: wire.Amount, CurrencyCode: wire.Currency, Receipt: wire.Receipt}, true
}

func isDuplicateOrderReceipt(body []byte) bool {
	var wire struct {
		Error struct {
			Description string `json:"description"`
		} `json:"error"`
	}
	return json.Unmarshal(body, &wire) == nil && strings.Contains(strings.ToLower(wire.Error.Description), "duplicate")
}

func orderFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "razorpay", Operation: "createOrder", Message: message}
}

func orderBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[OrderSummary] {
	failure := orderFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, OrderSummary{}, &failure, sdkgo.Receipt{})
}

func orderRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[OrderSummary] {
	return sdkgo.NewMutationRetry[OrderSummary](orderFailure(kind, message), retryAfter)
}
