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
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListPaymentsInput selects one bounded page of payments.
type ListPaymentsInput struct {
	// Count is the page size from 1 to 100.
	Count int `json:"count"`
	// Skip is the non-negative collection offset.
	Skip int `json:"skip"`
}

// PaymentPage contains selected safe fields from a payment collection.
type PaymentPage struct {
	// Payments contains only payment identity, status, amount, and currency.
	Payments []PaymentStatus `json:"payments"`
	// Count is the number of returned payments.
	Count int `json:"count"`
}

// ListPaymentsOperation reads a payment page.
type ListPaymentsOperation struct{ client *Client }

// ListPayments binds the Query to this client.
func (client *Client) ListPayments() ListPaymentsOperation { return ListPaymentsOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListPaymentsOperation) Definition() sdkgo.QueryDefinition { return ListPaymentsDefinition }

// Invoke lists payment summaries and excludes customer and card data.
func (operation ListPaymentsOperation) Invoke(call sdkgo.Call, input ListPaymentsInput) sdkgo.QueryAttempt[PaymentPage] {
	if input.Count < 1 || input.Count > 100 || input.Skip < 0 {
		return paymentPageBranch(ListPaymentsBranchDefect, sdkgo.FailureValidation, "Razorpay payment page is invalid")
	}
	query := url.Values{"count": {strconv.Itoa(input.Count)}, "skip": {strconv.Itoa(input.Skip)}}
	response := operation.client.get(call, "/payments?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return paymentPageBranch(ListPaymentsBranchDefect, sdkgo.FailureAuthentication, "Razorpay credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[PaymentPage](paymentOperationFailure("listPayments", sdkgo.FailureTransport, "Razorpay payment list request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[PaymentPage](paymentOperationFailure("listPayments", sdkgo.FailureRateLimit, "Razorpay rate limited payment listing"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[PaymentPage](paymentOperationFailure("listPayments", sdkgo.FailureAvailability, "Razorpay payment listing is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return paymentPageBranch(ListPaymentsBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected payment listing")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return paymentPageBranch(ListPaymentsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Razorpay payment list was oversized")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[PaymentPage](paymentOperationFailure("listPayments", sdkgo.FailureTransport, "Razorpay payment list was interrupted"), 0)
	}
	var collection struct {
		Entity string            `json:"entity"`
		Count  int               `json:"count"`
		Items  []json.RawMessage `json:"items"`
	}
	if json.Unmarshal(response.body, &collection) != nil || collection.Entity != "collection" || collection.Count < 0 || len(collection.Items) > input.Count {
		return paymentPageBranch(ListPaymentsBranchInvalidResponse, sdkgo.FailureProtocol, "Razorpay payment list is invalid")
	}
	page := PaymentPage{Payments: make([]PaymentStatus, 0, len(collection.Items)), Count: collection.Count}
	for _, item := range collection.Items {
		var identity struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(item, &identity) != nil {
			return paymentPageBranch(ListPaymentsBranchInvalidResponse, sdkgo.FailureProtocol, "Razorpay payment item is invalid")
		}
		payment, err := decodePayment(item, identity.ID)
		if err != nil || !validPaymentID(payment.ID) {
			return paymentPageBranch(ListPaymentsBranchInvalidResponse, sdkgo.FailureProtocol, "Razorpay payment item is invalid")
		}
		page.Payments = append(page.Payments, payment)
	}
	return sdkgo.NewQueryBranch(ListPaymentsBranchListed, page, nil, sdkgo.Receipt{})
}

// CapturePaymentInput names the authorized payment and full amount to capture.
type CapturePaymentInput struct {
	// PaymentID is the exact pay_ ID.
	PaymentID string `json:"paymentId"`
	// Amount is the full authorized amount in currency subunits.
	Amount int64 `json:"amount"`
	// CurrencyCode is the uppercase ISO 4217 code of the payment.
	CurrencyCode string `json:"currencyCode"`
}

// CapturePaymentOperation captures a payment once by its stable provider ID.
type CapturePaymentOperation struct{ client *Client }

// CapturePayment binds the Mutation to this client.
func (client *Client) CapturePayment() CapturePaymentOperation {
	return CapturePaymentOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CapturePaymentOperation) Definition() sdkgo.MutationDefinition { return CapturePaymentDefinition }

// IdempotencyKey records the call identity; Razorpay enforces one capture per payment ID.
func (CapturePaymentOperation) IdempotencyKey(callID sdkgo.CallID, _ CapturePaymentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke captures the full amount or confirms a completed capture by reading payment state.
func (operation CapturePaymentOperation) Invoke(call sdkgo.Call, input CapturePaymentInput) sdkgo.MutationAttempt[PaymentStatus] {
	if !validPaymentID(input.PaymentID) || input.Amount <= 0 || !validCurrencyCode(input.CurrencyCode) {
		return captureBranch(CapturePaymentBranchDefect, sdkgo.FailureValidation, "Razorpay capture input is invalid")
	}
	payload, _ := json.Marshal(map[string]any{"amount": input.Amount, "currency": input.CurrencyCode})
	response := operation.client.postCapture(call, input.PaymentID, payload)
	if errors.Is(response.err, errCredential) {
		return captureBranch(CapturePaymentBranchDefect, sdkgo.FailureAuthentication, "Razorpay credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return captureRetry(sdkgo.FailureTransport, "Razorpay capture request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return captureRetry(sdkgo.FailureRateLimit, "Razorpay rate limited capture", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return captureRetry(sdkgo.FailureAvailability, "Razorpay capture outcome is unknown", response.retryAfter)
	}
	if response.status == http.StatusBadRequest {
		return operation.confirmCapture(call, input)
	}
	if response.status != http.StatusOK {
		return captureBranch(CapturePaymentBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected capture")
	}
	if response.err != nil {
		return captureRetry(sdkgo.FailureTransport, "Razorpay capture response was interrupted", 0)
	}
	payment, err := decodePayment(response.body, input.PaymentID)
	if err != nil || payment.Status != "captured" || payment.Amount != input.Amount || payment.CurrencyCode != input.CurrencyCode {
		return captureRetry(sdkgo.FailureProtocol, "Razorpay capture may have completed but response was invalid", 0)
	}
	return sdkgo.NewMutationBranch(CapturePaymentBranchCaptured, payment, nil, sdkgo.Receipt{})
}

func (operation CapturePaymentOperation) confirmCapture(call sdkgo.Call, input CapturePaymentInput) sdkgo.MutationAttempt[PaymentStatus] {
	response := operation.client.get(call, "/payments/"+url.PathEscape(input.PaymentID))
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return captureRetry(sdkgo.FailureAvailability, "Razorpay capture state could not be checked", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return captureBranch(CapturePaymentBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected capture state lookup")
	}
	payment, err := decodePayment(response.body, input.PaymentID)
	if err != nil {
		return captureRetry(sdkgo.FailureProtocol, "Razorpay capture state is invalid", 0)
	}
	if payment.Status == "captured" && payment.Amount == input.Amount && payment.CurrencyCode == input.CurrencyCode {
		return sdkgo.NewMutationBranch(CapturePaymentBranchCaptured, payment, nil, sdkgo.Receipt{})
	}
	return captureBranch(CapturePaymentBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay payment is not captured for the requested amount")
}

func (client *Client) postCapture(call sdkgo.Call, paymentID string, payload []byte) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	keyID, keySecret := credentials.KeyID.Reveal(), credentials.KeySecret.Reveal()
	if !providerhttp.IsHeaderSafeCredential(keyID) || strings.Contains(keyID, ":") || !providerhttp.IsHeaderSafeCredential(keySecret) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/payments/"+url.PathEscape(paymentID)+"/capture", bytes.NewReader(payload))
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

func paymentOperationFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "razorpay", Operation: operation, Message: message}
}

func paymentPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PaymentPage] {
	failure := paymentOperationFailure("listPayments", kind, message)
	return sdkgo.NewQueryBranch(branch, PaymentPage{}, &failure, sdkgo.Receipt{})
}

func captureBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PaymentStatus] {
	failure := paymentOperationFailure("capturePayment", kind, message)
	return sdkgo.NewMutationBranch(branch, PaymentStatus{}, &failure, sdkgo.Receipt{})
}

func captureRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[PaymentStatus] {
	return sdkgo.NewMutationRetry[PaymentStatus](paymentOperationFailure("capturePayment", kind, message), retryAfter)
}
