// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package square

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListRefundsInput selects one refund page.
type ListRefundsInput struct {
	// Cursor continues a previous page; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// GetRefundInput identifies one Square refund.
type GetRefundInput struct {
	// RefundID is the Square refund ID.
	RefundID string `json:"refundId"`
}

// RefundPaymentInput requests a positive refund amount for a completed payment.
type RefundPaymentInput struct {
	// PaymentID is the completed Square payment to refund.
	PaymentID string `json:"paymentId"`
	// AmountMoney is the positive amount to refund in minor currency units.
	AmountMoney Money `json:"amountMoney"`
}

// Refund is a Square payment refund summary.
type Refund struct {
	// ID is the Square refund ID.
	ID string `json:"id"`
	// PaymentID is the payment associated with this refund.
	PaymentID string `json:"payment_id"`
	// Status is the provider refund status, such as PENDING or COMPLETED.
	Status string `json:"status"`
	// AmountMoney is the amount refunded in minor currency units.
	AmountMoney Money `json:"amount_money"`
}

// RefundPage is one page of Square refunds.
type RefundPage struct {
	// Refunds contains zero or more refunds.
	Refunds []Refund `json:"refunds"`
	// Cursor continues listing when set.
	Cursor string `json:"cursor"`
}

// ListRefundsOperation lists Square refunds.
type ListRefundsOperation struct{ client *Client }

// GetRefundOperation reads one Square refund.
type GetRefundOperation struct{ client *Client }

// RefundPaymentOperation requests a Square payment refund.
type RefundPaymentOperation struct{ client *Client }

// ListRefunds binds the listRefunds Query to this client.
func (client *Client) ListRefunds() ListRefundsOperation { return ListRefundsOperation{client} }

// GetRefund binds the getRefund Query to this client.
func (client *Client) GetRefund() GetRefundOperation { return GetRefundOperation{client} }

// RefundPayment binds the refundPayment Mutation to this client.
func (client *Client) RefundPayment() RefundPaymentOperation { return RefundPaymentOperation{client} }

// Definition returns the manifest-generated contract.
func (ListRefundsOperation) Definition() sdkgo.QueryDefinition { return ListRefundsDefinition }

// Definition returns the manifest-generated contract.
func (GetRefundOperation) Definition() sdkgo.QueryDefinition { return GetRefundDefinition }

// Definition returns the manifest-generated contract.
func (RefundPaymentOperation) Definition() sdkgo.MutationDefinition { return RefundPaymentDefinition }

// IdempotencyKey derives a Square-compatible key from the stable Dex call ID.
func (RefundPaymentOperation) IdempotencyKey(callID sdkgo.CallID, _ RefundPaymentInput) sdkgo.IdempotencyKey {
	return squarePaymentKey(callID)
}

// Invoke lists one page and classifies provider failures.
func (operation ListRefundsOperation) Invoke(call sdkgo.Call, input ListRefundsInput) sdkgo.QueryAttempt[RefundPage] {
	query := url.Values{}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	return executeRead(operation.client, call, "listRefunds", "/refunds", query, ListRefundsBranchListed, ListRefundsBranchNotFound, ListRefundsBranchProviderRejected, ListRefundsBranchInvalidResponse, ListRefundsBranchDefect, decodeRefundPage)
}

func decodeRefundPage(body []byte) (RefundPage, error) {
	var page RefundPage
	if err := json.Unmarshal(body, &page); err != nil {
		return RefundPage{}, err
	}
	if page.Refunds == nil {
		page.Refunds = []Refund{}
	}
	return page, nil
}

// Invoke reads one Square refund by ID.
func (operation GetRefundOperation) Invoke(call sdkgo.Call, input GetRefundInput) sdkgo.QueryAttempt[Refund] {
	if !validPathSegment(input.RefundID) {
		return failedAttempt[Refund]("getRefund", GetRefundBranchDefect, sdkgo.FailureValidation, "refundId must be a path segment")
	}
	return executeRead(operation.client, call, "getRefund", "/refunds/"+url.PathEscape(input.RefundID), url.Values{}, GetRefundBranchFound, GetRefundBranchNotFound, GetRefundBranchProviderRejected, GetRefundBranchInvalidResponse, GetRefundBranchDefect, decodeRefund)
}

func decodeRefund(body []byte) (Refund, error) {
	var response struct {
		Refund Refund `json:"refund"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Refund.ID == "" {
		return Refund{}, errors.New("missing refund")
	}
	return response.Refund, nil
}

// Invoke sends one keyed refund request and retries unknown outcomes with the same key.
func (operation RefundPaymentOperation) Invoke(call sdkgo.Call, input RefundPaymentInput) sdkgo.MutationAttempt[Refund] {
	if !validPathSegment(input.PaymentID) || input.AmountMoney.Amount <= 0 || input.AmountMoney.Currency == "" {
		return refundDefect()
	}
	payload, err := json.Marshal(map[string]any{"payment_id": input.PaymentID, "amount_money": input.AmountMoney, "idempotency_key": call.IdempotencyKey})
	if err != nil {
		return refundDefect()
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/refunds", bytes.NewReader(payload))
	if err != nil {
		return refundDefect()
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	authorization := operation.client.authorize(call, "refundPayment", request, RefundPaymentBranchDefect, RefundPaymentBranchProviderRejected, RefundPaymentBranchInvalidResponse)
	if authorization.failure != nil {
		return sdkgo.NewMutationBranch(authorization.branch, Refund{}, authorization.failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[Refund](*failureFor("refundPayment", sdkgo.FailureAvailability, "provider refund response was unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[Refund](*failureFor("refundPayment", sdkgo.FailureRateLimit, "provider rate limited the refund"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[Refund](*failureFor("refundPayment", sdkgo.FailureAvailability, "provider refund is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusUnauthorized:
		return sdkgo.NewMutationBranch(RefundPaymentBranchProviderRejected, Refund{}, failureFor("refundPayment", sdkgo.FailureAuthentication, "provider rejected credentials"), sdkgo.Receipt{})
	case response.StatusCode == http.StatusForbidden:
		return sdkgo.NewMutationBranch(RefundPaymentBranchProviderRejected, Refund{}, failureFor("refundPayment", sdkgo.FailureAuthorization, "provider denied access"), sdkgo.Receipt{})
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return sdkgo.NewMutationBranch(RefundPaymentBranchProviderRejected, Refund{}, failureFor("refundPayment", sdkgo.FailureProviderRejection, "provider rejected refund with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewMutationBranch(RefundPaymentBranchInvalidResponse, Refund{}, failureFor("refundPayment", sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit"), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewMutationRetry[Refund](*failureFor("refundPayment", sdkgo.FailureAvailability, "provider refund response was interrupted"), 0)
	}
	refund, err := decodeRefund(body)
	if err != nil {
		return sdkgo.NewMutationBranch(RefundPaymentBranchInvalidResponse, Refund{}, failureFor("refundPayment", sdkgo.FailureProtocol, "provider returned invalid JSON"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(RefundPaymentBranchRefunded, refund, nil, sdkgo.Receipt{})
}

func refundDefect() sdkgo.MutationAttempt[Refund] {
	return sdkgo.NewMutationBranch(RefundPaymentBranchDefect, Refund{}, failureFor("refundPayment", sdkgo.FailureValidation, "refund input is invalid"), sdkgo.Receipt{})
}
