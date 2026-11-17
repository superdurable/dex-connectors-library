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
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// RefundStatus contains refund identity and state without customer or bank details.
type RefundStatus struct {
	// ID is the Razorpay refund ID.
	ID string `json:"id"`
	// PaymentID identifies the refunded payment.
	PaymentID string `json:"paymentId"`
	// Status is the refund state such as pending or processed.
	Status string `json:"status"`
	// Amount is the refund amount in currency subunits.
	Amount int64 `json:"amount"`
	// CurrencyCode is the ISO 4217 currency code.
	CurrencyCode string `json:"currencyCode"`
}

// ListRefundsInput selects one bounded refund page.
type ListRefundsInput struct {
	// Count is the page size from 1 to 100.
	Count int `json:"count"`
	// Skip is the non-negative collection offset.
	Skip int `json:"skip"`
}

// RefundPage contains selected refund statuses.
type RefundPage struct {
	// Refunds contains only safe refund fields.
	Refunds []RefundStatus `json:"refunds"`
	// Count is the number of returned refunds.
	Count int `json:"count"`
}

// GetRefundInput selects a refund by its ID.
type GetRefundInput struct {
	// RefundID is the exact ID prefixed rfnd_.
	RefundID string `json:"refundId"`
}

// CreateNormalRefundInput identifies one captured payment and a positive refund amount.
type CreateNormalRefundInput struct {
	// PaymentID is the exact pay_ ID.
	PaymentID string `json:"paymentId"`
	// Amount is the requested refund in currency subunits.
	Amount int64 `json:"amount"`
}

// ListRefundsOperation reads one refund page.
type ListRefundsOperation struct{ client *Client }

// ListRefunds binds the Query to this client.
func (client *Client) ListRefunds() ListRefundsOperation { return ListRefundsOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListRefundsOperation) Definition() sdkgo.QueryDefinition { return ListRefundsDefinition }

// Invoke lists safe refund summaries.
func (operation ListRefundsOperation) Invoke(call sdkgo.Call, input ListRefundsInput) sdkgo.QueryAttempt[RefundPage] {
	if input.Count < 1 || input.Count > 100 || input.Skip < 0 {
		return refundPageBranch(ListRefundsBranchDefect, sdkgo.FailureValidation, "Razorpay refund page is invalid")
	}
	query := url.Values{"count": {strconv.Itoa(input.Count)}, "skip": {strconv.Itoa(input.Skip)}}
	response := operation.client.get(call, "/refunds?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return refundPageBranch(ListRefundsBranchDefect, sdkgo.FailureAuthentication, "Razorpay credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[RefundPage](refundFailure("listRefunds", sdkgo.FailureTransport, "Razorpay refund list request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[RefundPage](refundFailure("listRefunds", sdkgo.FailureRateLimit, "Razorpay rate limited refund listing"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[RefundPage](refundFailure("listRefunds", sdkgo.FailureAvailability, "Razorpay refund listing is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return refundPageBranch(ListRefundsBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected refund listing")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return refundPageBranch(ListRefundsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Razorpay refund list was oversized")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[RefundPage](refundFailure("listRefunds", sdkgo.FailureTransport, "Razorpay refund list was interrupted"), 0)
	}
	var collection struct {
		Entity string            `json:"entity"`
		Count  int               `json:"count"`
		Items  []json.RawMessage `json:"items"`
	}
	if json.Unmarshal(response.body, &collection) != nil || collection.Entity != "collection" || collection.Count < 0 || len(collection.Items) > input.Count {
		return refundPageBranch(ListRefundsBranchInvalidResponse, sdkgo.FailureProtocol, "Razorpay refund list is invalid")
	}
	page := RefundPage{Refunds: make([]RefundStatus, 0, len(collection.Items)), Count: collection.Count}
	for _, item := range collection.Items {
		refund, valid := decodeRefundStatus(item)
		if !valid {
			return refundPageBranch(ListRefundsBranchInvalidResponse, sdkgo.FailureProtocol, "Razorpay refund item is invalid")
		}
		page.Refunds = append(page.Refunds, refund)
	}
	return sdkgo.NewQueryBranch(ListRefundsBranchListed, page, nil, sdkgo.Receipt{})
}

// GetRefundOperation reads one refund.
type GetRefundOperation struct{ client *Client }

// GetRefund binds the Query to this client.
func (client *Client) GetRefund() GetRefundOperation { return GetRefundOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetRefundOperation) Definition() sdkgo.QueryDefinition { return GetRefundDefinition }

// Invoke reads a refund and selects only safe fields.
func (operation GetRefundOperation) Invoke(call sdkgo.Call, input GetRefundInput) sdkgo.QueryAttempt[RefundStatus] {
	if !refundIDPattern.MatchString(input.RefundID) {
		return getRefundBranch(GetRefundBranchDefect, sdkgo.FailureValidation, "Razorpay refund ID is invalid")
	}
	response := operation.client.get(call, "/refunds/"+url.PathEscape(input.RefundID))
	if errors.Is(response.err, errCredential) {
		return getRefundBranch(GetRefundBranchDefect, sdkgo.FailureAuthentication, "Razorpay credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[RefundStatus](refundFailure("getRefund", sdkgo.FailureTransport, "Razorpay refund request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[RefundStatus](refundFailure("getRefund", sdkgo.FailureRateLimit, "Razorpay rate limited refund lookup"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[RefundStatus](refundFailure("getRefund", sdkgo.FailureAvailability, "Razorpay refund lookup is unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return getRefundBranch(GetRefundBranchNotFound, sdkgo.FailureNotFound, "Razorpay refund was not found")
	}
	if response.status != http.StatusOK {
		return getRefundBranch(GetRefundBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected refund lookup")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return getRefundBranch(GetRefundBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Razorpay refund response was oversized")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[RefundStatus](refundFailure("getRefund", sdkgo.FailureTransport, "Razorpay refund response was interrupted"), 0)
	}
	refund, valid := decodeRefundStatus(response.body)
	if !valid || refund.ID != input.RefundID {
		return getRefundBranch(GetRefundBranchInvalidResponse, sdkgo.FailureProtocol, "Razorpay refund response is invalid")
	}
	return sdkgo.NewQueryBranch(GetRefundBranchFound, refund, nil, sdkgo.Receipt{})
}

// CreateNormalRefundOperation issues an idempotent normal refund.
type CreateNormalRefundOperation struct{ client *Client }

// CreateNormalRefund binds the Mutation to this client.
func (client *Client) CreateNormalRefund() CreateNormalRefundOperation {
	return CreateNormalRefundOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CreateNormalRefundOperation) Definition() sdkgo.MutationDefinition {
	return CreateNormalRefundDefinition
}

// IdempotencyKey uses the stable Dex Call ID for Razorpay's refund deduplication header.
func (CreateNormalRefundOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateNormalRefundInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a refund with a stable idempotency key and identical body on every retry.
func (operation CreateNormalRefundOperation) Invoke(call sdkgo.Call, input CreateNormalRefundInput) sdkgo.MutationAttempt[RefundStatus] {
	if !validPaymentID(input.PaymentID) || input.Amount <= 0 {
		return createRefundBranch(CreateNormalRefundBranchDefect, sdkgo.FailureValidation, "Razorpay refund input is invalid")
	}
	payload, _ := json.Marshal(map[string]int64{"amount": input.Amount})
	response := operation.client.postNormalRefund(call, input.PaymentID, payload)
	if errors.Is(response.err, errCredential) {
		return createRefundBranch(CreateNormalRefundBranchDefect, sdkgo.FailureAuthentication, "Razorpay credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return createRefundRetry(sdkgo.FailureTransport, "Razorpay refund request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return createRefundRetry(sdkgo.FailureRateLimit, "Razorpay rate limited refund creation", response.retryAfter)
	}
	if response.status == http.StatusConflict || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return createRefundRetry(sdkgo.FailureAvailability, "Razorpay refund outcome is pending or unknown", response.retryAfter)
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		return createRefundBranch(CreateNormalRefundBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected the normal refund")
	}
	if response.err != nil {
		return createRefundRetry(sdkgo.FailureTransport, "Razorpay refund response was interrupted", 0)
	}
	refund, valid := decodeRefundStatus(response.body)
	if !valid || refund.PaymentID != input.PaymentID || refund.Amount != input.Amount {
		return createRefundRetry(sdkgo.FailureProtocol, "Razorpay refund may exist but response was invalid", 0)
	}
	return sdkgo.NewMutationBranch(CreateNormalRefundBranchCreated, refund, nil, sdkgo.Receipt{})
}

func (client *Client) postNormalRefund(call sdkgo.Call, paymentID string, payload []byte) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	keyID, keySecret := credentials.KeyID.Reveal(), credentials.KeySecret.Reveal()
	if !providerhttp.IsHeaderSafeCredential(keyID) || strings.Contains(keyID, ":") || !providerhttp.IsHeaderSafeCredential(keySecret) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/payments/"+url.PathEscape(paymentID)+"/refund", bytes.NewReader(payload))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(keyID+":"+keySecret)))
	request.Header.Set("X-Refund-Idempotency", string(call.IdempotencyKey))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}

var refundIDPattern = regexp.MustCompile(`^rfnd_[A-Za-z0-9]{6,64}$`)

func decodeRefundStatus(body []byte) (RefundStatus, bool) {
	var wire struct {
		ID        string `json:"id"`
		Entity    string `json:"entity"`
		PaymentID string `json:"payment_id"`
		Status    string `json:"status"`
		Amount    int64  `json:"amount"`
		Currency  string `json:"currency"`
	}
	if json.Unmarshal(body, &wire) != nil || !refundIDPattern.MatchString(wire.ID) || wire.Entity != "refund" || !validPaymentID(wire.PaymentID) || wire.Status == "" || wire.Amount <= 0 || !validCurrencyCode(wire.Currency) {
		return RefundStatus{}, false
	}
	return RefundStatus{ID: wire.ID, PaymentID: wire.PaymentID, Status: wire.Status, Amount: wire.Amount, CurrencyCode: wire.Currency}, true
}

func refundFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "razorpay", Operation: operation, Message: message}
}

func refundPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[RefundPage] {
	failure := refundFailure("listRefunds", kind, message)
	return sdkgo.NewQueryBranch(branch, RefundPage{}, &failure, sdkgo.Receipt{})
}

func getRefundBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[RefundStatus] {
	failure := refundFailure("getRefund", kind, message)
	return sdkgo.NewQueryBranch(branch, RefundStatus{}, &failure, sdkgo.Receipt{})
}

func createRefundBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[RefundStatus] {
	failure := refundFailure("createNormalRefund", kind, message)
	return sdkgo.NewMutationBranch(branch, RefundStatus{}, &failure, sdkgo.Receipt{})
}

func createRefundRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[RefundStatus] {
	return sdkgo.NewMutationRetry[RefundStatus](refundFailure("createNormalRefund", kind, message), retryAfter)
}
