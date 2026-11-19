// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paypal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetPayoutBatchInput identifies one payout batch and optional item page.
type GetPayoutBatchInput struct {
	// BatchID is the PayPal payout batch ID.
	BatchID string `json:"batchId"`
	// Page is the one-based item page; zero uses PayPal's first page.
	Page int `json:"page,omitempty"`
}

// GetPayoutItemInput identifies one payout item.
type GetPayoutItemInput struct {
	// ItemID is the PayPal payout item ID.
	ItemID string `json:"itemId"`
}

// CancelPayoutItemInput identifies an unclaimed payout item.
type CancelPayoutItemInput struct {
	// ItemID is the PayPal payout item ID.
	ItemID string `json:"itemId"`
}

// PayoutAmount is a decimal monetary amount and ISO currency code.
type PayoutAmount struct {
	// Currency is the three-letter ISO currency code.
	Currency string `json:"currency"`
	// Value is a positive decimal amount in that currency's units.
	Value string `json:"value"`
}

// PayoutRecipient identifies a recipient and amount.
type PayoutRecipient struct {
	// RecipientType is EMAIL, PHONE, or PAYPAL_ID.
	RecipientType string `json:"recipient_type"`
	// Receiver is the email, phone number, or PayPal account ID.
	Receiver string `json:"receiver"`
	// Amount is the payout amount.
	Amount PayoutAmount `json:"amount"`
	// SenderItemID is an optional caller tracking ID.
	SenderItemID string `json:"sender_item_id,omitempty"`
}

// CreatePayoutBatchInput contains the items sent as one payout batch.
type CreatePayoutBatchInput struct {
	// EmailSubject is the subject of recipient notifications.
	EmailSubject string `json:"emailSubject"`
	// Items contains one or more payout recipients.
	Items []PayoutRecipient `json:"items"`
}

// PayoutBatchHeader summarizes a PayPal payout batch.
type PayoutBatchHeader struct {
	// PayoutBatchID is PayPal's batch ID.
	PayoutBatchID string `json:"payout_batch_id"`
	// BatchStatus is PayPal's current batch state.
	BatchStatus string `json:"batch_status"`
}

// PayoutBatch contains batch status and an optional item page.
type PayoutBatch struct {
	// BatchHeader contains the batch ID and status.
	BatchHeader PayoutBatchHeader `json:"batch_header"`
	// Items is the returned page of payout items.
	Items []PayoutItem `json:"items,omitempty"`
	// TotalPages is the number of available item pages.
	TotalPages int `json:"total_pages,omitempty"`
}

// PayoutItem summarizes a PayPal payout item.
type PayoutItem struct {
	// PayoutItemID is PayPal's item ID.
	PayoutItemID string `json:"payout_item_id"`
	// TransactionStatus is PayPal's current item state.
	TransactionStatus string `json:"transaction_status"`
	// PayoutBatchID is the parent payout batch ID.
	PayoutBatchID string `json:"payout_batch_id,omitempty"`
}

// GetPayoutBatchOperation reads payout batch details.
type GetPayoutBatchOperation struct{ client *Client }

// GetPayoutBatch binds the payout batch read to this client.
func (client *Client) GetPayoutBatch() GetPayoutBatchOperation {
	return GetPayoutBatchOperation{client}
}

// Definition returns the generated payout batch contract.
func (GetPayoutBatchOperation) Definition() sdkgo.QueryDefinition { return GetPayoutBatchDefinition }

// Invoke reads a payout batch and its item page.
func (operation GetPayoutBatchOperation) Invoke(call sdkgo.Call, input GetPayoutBatchInput) sdkgo.QueryAttempt[PayoutBatch] {
	if !validPathSegment(input.BatchID) || input.Page < 0 || input.Page > 1000 {
		return failedAttempt[PayoutBatch]("getPayoutBatch", GetPayoutBatchBranchDefect, sdkgo.FailureValidation, "batchId or page is invalid")
	}
	query := url.Values{}
	if input.Page > 0 {
		query.Set("page", strconv.Itoa(input.Page))
	}
	return executeRead(operation.client, call, "getPayoutBatch", "/v1/payments/payouts/"+url.PathEscape(input.BatchID), query, GetPayoutBatchBranchFound, GetPayoutBatchBranchNotFound, GetPayoutBatchBranchProviderRejected, GetPayoutBatchBranchInvalidResponse, GetPayoutBatchBranchDefect, func(body []byte) (PayoutBatch, error) {
		var batch PayoutBatch
		err := json.Unmarshal(body, &batch)
		if err == nil && batch.BatchHeader.PayoutBatchID == "" {
			err = fmt.Errorf("missing batch ID")
		}
		return batch, err
	})
}

// GetPayoutItemOperation reads one payout item.
type GetPayoutItemOperation struct{ client *Client }

// GetPayoutItem binds the payout item read to this client.
func (client *Client) GetPayoutItem() GetPayoutItemOperation { return GetPayoutItemOperation{client} }

// Definition returns the generated payout item contract.
func (GetPayoutItemOperation) Definition() sdkgo.QueryDefinition { return GetPayoutItemDefinition }

// Invoke reads a payout item.
func (operation GetPayoutItemOperation) Invoke(call sdkgo.Call, input GetPayoutItemInput) sdkgo.QueryAttempt[PayoutItem] {
	if !validPathSegment(input.ItemID) {
		return failedAttempt[PayoutItem]("getPayoutItem", GetPayoutItemBranchDefect, sdkgo.FailureValidation, "itemId is invalid")
	}
	return executeRead(operation.client, call, "getPayoutItem", "/v1/payments/payouts-item/"+url.PathEscape(input.ItemID), nil, GetPayoutItemBranchFound, GetPayoutItemBranchNotFound, GetPayoutItemBranchProviderRejected, GetPayoutItemBranchInvalidResponse, GetPayoutItemBranchDefect, decodePayoutItem)
}

// CreatePayoutBatchOperation creates a payout with PayPal's 30-day sender ID deduplication.
type CreatePayoutBatchOperation struct{ client *Client }

// CreatePayoutBatch binds payout creation to this client.
func (client *Client) CreatePayoutBatch() CreatePayoutBatchOperation {
	return CreatePayoutBatchOperation{client}
}

// Definition returns the generated payout creation contract.
func (CreatePayoutBatchOperation) Definition() sdkgo.MutationDefinition {
	return CreatePayoutBatchDefinition
}

// IdempotencyKey uses the Dex call ID as PayPal's stable sender batch ID.
func (CreatePayoutBatchOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePayoutBatchInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates the payout batch; PayPal deduplicates its sender ID for 30 days.
func (operation CreatePayoutBatchOperation) Invoke(call sdkgo.Call, input CreatePayoutBatchInput) sdkgo.MutationAttempt[PayoutBatch] {
	if call.IdempotencyKey == "" || len(call.IdempotencyKey) > 256 || input.EmailSubject == "" || len(input.Items) == 0 || len(input.Items) > 15000 {
		return payoutFailure[PayoutBatch]("createPayoutBatch", CreatePayoutBatchBranchDefect, sdkgo.FailureValidation, "payout header or items are invalid")
	}
	for _, item := range input.Items {
		if item.Receiver == "" || item.Amount.Currency == "" || item.Amount.Value == "" || (item.RecipientType != "EMAIL" && item.RecipientType != "PHONE" && item.RecipientType != "PAYPAL_ID") {
			return payoutFailure[PayoutBatch]("createPayoutBatch", CreatePayoutBatchBranchDefect, sdkgo.FailureValidation, "payout item is invalid")
		}
	}
	body, err := json.Marshal(struct {
		Header struct {
			SenderBatchID string `json:"sender_batch_id"`
			EmailSubject  string `json:"email_subject"`
		} `json:"sender_batch_header"`
		Items []PayoutRecipient `json:"items"`
	}{Header: struct {
		SenderBatchID string `json:"sender_batch_id"`
		EmailSubject  string `json:"email_subject"`
	}{SenderBatchID: string(call.IdempotencyKey), EmailSubject: input.EmailSubject}, Items: input.Items})
	if err != nil {
		return payoutFailure[PayoutBatch]("createPayoutBatch", CreatePayoutBatchBranchDefect, sdkgo.FailureValidation, "payout cannot be encoded")
	}
	return executePayoutWrite(operation.client, call, "createPayoutBatch", http.MethodPost, "/v1/payments/payouts", body, CreatePayoutBatchBranchCreated, "", CreatePayoutBatchBranchProviderRejected, CreatePayoutBatchBranchInvalidResponse, CreatePayoutBatchBranchDefect, func(body []byte) (PayoutBatch, error) {
		var batch PayoutBatch
		err := json.Unmarshal(body, &batch)
		if err == nil && batch.BatchHeader.PayoutBatchID == "" {
			err = fmt.Errorf("missing batch ID")
		}
		return batch, err
	})
}

// CancelPayoutItemOperation cancels one unclaimed payout item.
type CancelPayoutItemOperation struct{ client *Client }

// CancelPayoutItem binds payout cancellation to this client.
func (client *Client) CancelPayoutItem() CancelPayoutItemOperation {
	return CancelPayoutItemOperation{client}
}

// Definition returns the generated payout cancellation contract.
func (CancelPayoutItemOperation) Definition() sdkgo.MutationDefinition {
	return CancelPayoutItemDefinition
}

// IdempotencyKey derives a stable Dex call key for the cancellation receipt.
func (CancelPayoutItemOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelPayoutItemInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke cancels an unclaimed item; a returned item is already canceled.
func (operation CancelPayoutItemOperation) Invoke(call sdkgo.Call, input CancelPayoutItemInput) sdkgo.MutationAttempt[PayoutItem] {
	if !validPathSegment(input.ItemID) {
		return payoutFailure[PayoutItem]("cancelPayoutItem", CancelPayoutItemBranchDefect, sdkgo.FailureValidation, "itemId is invalid")
	}
	return executePayoutWrite(operation.client, call, "cancelPayoutItem", http.MethodPost, "/v1/payments/payouts-item/"+url.PathEscape(input.ItemID)+"/cancel", nil, CancelPayoutItemBranchCanceled, CancelPayoutItemBranchNotFound, CancelPayoutItemBranchProviderRejected, CancelPayoutItemBranchInvalidResponse, CancelPayoutItemBranchDefect, decodePayoutItem)
}

func decodePayoutItem(body []byte) (PayoutItem, error) {
	var item PayoutItem
	err := json.Unmarshal(body, &item)
	if err == nil && item.PayoutItemID == "" {
		err = fmt.Errorf("missing item ID")
	}
	return item, err
}

func payoutFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationBranch(branch, *new(T), failureFor(operation, kind, message), sdkgo.Receipt{})
}

func executePayoutWrite[T any](client *Client, call sdkgo.Call, operation, method, path string, body []byte, successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.MutationAttempt[T] {
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return payoutFailure[T](operation, defectBranch, sdkgo.FailureLocalDefect, "provider request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	auth := client.authorize(call, operation, request, defectBranch, rejectedBranch, invalidBranch)
	if auth.failure != nil {
		if auth.retry {
			return sdkgo.NewMutationRetry[T](*auth.failure, auth.retryAfter)
		}
		return sdkgo.NewMutationBranch(auth.branch, *new(T), auth.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[T](*failureFor(operation, sdkgo.FailureAvailability, "payout outcome is unknown"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[T](*failureFor(operation, sdkgo.FailureRateLimit, "provider rate limited the payout"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return sdkgo.NewMutationRetry[T](*failureFor(operation, sdkgo.FailureAvailability, "payout outcome is unknown"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusNotFound && notFoundBranch != "" {
		return payoutFailure[T](operation, notFoundBranch, sdkgo.FailureNotFound, "payout item was not found")
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusAccepted {
		return payoutFailure[T](operation, rejectedBranch, sdkgo.FailureProviderRejection, "provider rejected the payout action with HTTP "+strconv.Itoa(response.StatusCode))
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if err != nil {
		return payoutFailure[T](operation, invalidBranch, sdkgo.FailureProtocol, "provider payout response was invalid")
	}
	value, err := decode(content)
	if err != nil {
		return payoutFailure[T](operation, invalidBranch, sdkgo.FailureProtocol, "provider payout response was invalid")
	}
	return sdkgo.NewMutationBranch(successBranch, value, nil, sdkgo.Receipt{IdempotencyKey: call.IdempotencyKey})
}
