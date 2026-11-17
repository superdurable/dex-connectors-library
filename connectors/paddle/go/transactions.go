// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paddle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// ListTransactionsInput selects one page of Paddle transactions.
type ListTransactionsInput struct {
	// PerPage is the page size from 1 through 30.
	PerPage int `json:"perPage"`
	// After is an optional txn_ ID from the previous page's cursor.
	After string `json:"after,omitempty"`
	// Status optionally filters by one Paddle transaction status.
	Status string `json:"status,omitempty"`
}

// TransactionPage contains one page of transaction states.
type TransactionPage struct {
	// Items are transaction identities and states on this page.
	Items []TransactionStatus `json:"items"`
	// HasMore reports whether another page follows.
	HasMore bool `json:"hasMore"`
	// NextCursor is the next page's txn_ ID or blank after the last page.
	NextCursor string `json:"nextCursor"`
}

// CreateTransactionInput creates a draft transaction with one catalog price.
type CreateTransactionInput struct {
	// PriceID is a pri_ catalog price ID.
	PriceID string `json:"priceId"`
	// Quantity is the positive number of units.
	Quantity int `json:"quantity"`
}

// UpdateTransactionInput attaches a customer and billing address to a draft transaction.
type UpdateTransactionInput struct {
	// TransactionID identifies a draft or ready transaction.
	TransactionID string `json:"transactionId"`
	// CustomerID identifies the customer to bill.
	CustomerID string `json:"customerId"`
	// AddressID identifies that customer's billing address.
	AddressID string `json:"addressId"`
}

// ListTransactionsOperation lists Paddle transactions.
type ListTransactionsOperation struct{ client *Client }

// CreateTransactionOperation creates a draft transaction.
type CreateTransactionOperation struct{ client *Client }

// UpdateTransactionOperation attaches billing identity to a draft transaction.
type UpdateTransactionOperation struct{ client *Client }

// CancelTransactionOperation cancels an eligible transaction.
type CancelTransactionOperation struct{ client *Client }

// ListTransactions binds the listing Query.
func (client *Client) ListTransactions() ListTransactionsOperation {
	return ListTransactionsOperation{client}
}

// CreateTransaction binds the creation Mutation.
func (client *Client) CreateTransaction() CreateTransactionOperation {
	return CreateTransactionOperation{client}
}

// UpdateTransaction binds the update Mutation.
func (client *Client) UpdateTransaction() UpdateTransactionOperation {
	return UpdateTransactionOperation{client}
}

// CancelTransaction binds the cancellation Mutation.
func (client *Client) CancelTransaction() CancelTransactionOperation {
	return CancelTransactionOperation{client}
}

// Definition returns the generated listing contract.
func (ListTransactionsOperation) Definition() sdkgo.QueryDefinition {
	return ListTransactionsDefinition
}

// Definition returns the generated creation contract.
func (CreateTransactionOperation) Definition() sdkgo.MutationDefinition {
	return CreateTransactionDefinition
}

// Definition returns the generated update contract.
func (UpdateTransactionOperation) Definition() sdkgo.MutationDefinition {
	return UpdateTransactionDefinition
}

// Definition returns the generated cancellation contract.
func (CancelTransactionOperation) Definition() sdkgo.MutationDefinition {
	return CancelTransactionDefinition
}

// IdempotencyKey records call identity; Paddle does not deduplicate transaction creation on this key.
func (CreateTransactionOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTransactionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records update call identity.
func (UpdateTransactionOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateTransactionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records cancellation call identity.
func (CancelTransactionOperation) IdempotencyKey(callID sdkgo.CallID, _ GetTransactionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

func validTransactionStatus(status string) bool {
	switch status {
	case "draft", "ready", "billed", "paid", "completed", "canceled", "past_due":
		return true
	default:
		return false
	}
}

func decodeTransactionPage(body []byte) (TransactionPage, bool) {
	var envelope struct {
		Data []struct {
			ID           string `json:"id"`
			Status       string `json:"status"`
			CurrencyCode string `json:"currency_code"`
		} `json:"data"`
		Meta struct {
			Pagination struct {
				Next    string `json:"next"`
				HasMore bool   `json:"has_more"`
			} `json:"pagination"`
		} `json:"meta"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Data == nil {
		return TransactionPage{}, false
	}
	page := TransactionPage{Items: make([]TransactionStatus, 0, len(envelope.Data)), HasMore: envelope.Meta.Pagination.HasMore}
	for _, item := range envelope.Data {
		if !validTransactionID(item.ID) || !validTransactionStatus(item.Status) || len(item.CurrencyCode) != 3 {
			return TransactionPage{}, false
		}
		page.Items = append(page.Items, TransactionStatus{ID: item.ID, Status: item.Status, CurrencyCode: item.CurrencyCode})
	}
	if page.HasMore {
		next, err := url.Parse(envelope.Meta.Pagination.Next)
		if err != nil || next.Path != "/transactions" || !validTransactionID(next.Query().Get("after")) {
			return TransactionPage{}, false
		}
		page.NextCursor = next.Query().Get("after")
	}
	return page, true
}

func transactionOperationFailure(name string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "paddle", Operation: name, Message: message}
}

// Invoke reads one transaction page.
func (operation ListTransactionsOperation) Invoke(call sdkgo.Call, input ListTransactionsInput) sdkgo.QueryAttempt[TransactionPage] {
	const name = "listTransactions"
	if input.PerPage < 1 || input.PerPage > 30 || input.After != "" && !validTransactionID(input.After) || input.Status != "" && !validTransactionStatus(input.Status) {
		return sdkgo.NewQueryBranch(ListTransactionsBranchDefect, TransactionPage{}, transactionOperationFailure(name, sdkgo.FailureValidation, "transaction page input is invalid"), sdkgo.Receipt{})
	}
	query := url.Values{"per_page": {strconv.Itoa(input.PerPage)}}
	if input.After != "" {
		query.Set("after", input.After)
	}
	if input.Status != "" {
		query.Set("status", input.Status)
	}
	exchange := operation.client.exchange(call, call.Context, http.MethodGet, "/transactions?"+query.Encode(), nil)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewQueryBranch(ListTransactionsBranchDefect, TransactionPage{}, transactionOperationFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewQueryRetry[TransactionPage](*transactionOperationFailure(name, sdkgo.FailureAvailability, "transaction listing is unavailable"), exchange.retryAfter)
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewQueryBranch(ListTransactionsBranchProviderRejected, TransactionPage{}, transactionOperationFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected transaction listing"), sdkgo.Receipt{})
	}
	page, ok := decodeTransactionPage(exchange.body)
	if !ok || exchange.err != nil {
		return sdkgo.NewQueryBranch(ListTransactionsBranchInvalidResponse, TransactionPage{}, transactionOperationFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid transaction page"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(ListTransactionsBranchListed, page, nil, sdkgo.Receipt{})
}

// Invoke creates a draft transaction with a bounded number of possible sends.
func (operation CreateTransactionOperation) Invoke(call sdkgo.Call, input CreateTransactionInput) sdkgo.MutationAttempt[TransactionStatus] {
	const name = "createTransaction"
	if !validPaddleID(input.PriceID, "pri_") || input.Quantity < 1 || input.Quantity > 100000 {
		return sdkgo.NewMutationBranch(CreateTransactionBranchDefect, TransactionStatus{}, transactionOperationFailure(name, sdkgo.FailureValidation, "price ID or quantity is invalid"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateTransactionDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateTransactionBranchDefect, TransactionStatus{}, transactionOperationFailure(name, sdkgo.FailureLocalDefect, "transaction dispatch fence is unavailable"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Marker.ProviderID != "" {
		readBack := operation.client.exchange(call, call.Context, http.MethodGet, "/transactions/"+url.PathEscape(decision.Marker.ProviderID), nil)
		if readBack.status == http.StatusOK && readBack.err == nil {
			transaction, decodeErr := decodeTransaction(readBack.body, decision.Marker.ProviderID)
			if decodeErr == nil {
				return sdkgo.NewMutationBranch(CreateTransactionBranchCreated, transaction, nil, sdkgo.Receipt{ProviderObjectID: transaction.ID})
			}
		}
		if paddleRetryable(readBack) {
			return sdkgo.NewMutationRetry[TransactionStatus](*transactionOperationFailure(name, sdkgo.FailureAvailability, "transaction read-back is unavailable"), readBack.retryAfter)
		}
		return sdkgo.NewMutationUncertain(TransactionStatus{}, *transactionOperationFailure(name, sdkgo.FailureProtocol, "created transaction could not be confirmed"), sdkgo.Receipt{})
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired || decision.MaximumPriorSends >= call.MaxSends() {
		return sdkgo.NewMutationUncertain(TransactionStatus{}, *transactionOperationFailure(name, sdkgo.FailureTransport, "transaction creation outcome is unknown"), sdkgo.Receipt{})
	}
	payload := map[string]any{"items": []map[string]any{{"price_id": input.PriceID, "quantity": input.Quantity}}}
	var exchange paddleExchange
	err = fence.Dispatch(func(ctx context.Context) error {
		exchange = operation.client.exchange(call, ctx, http.MethodPost, "/transactions", payload)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[TransactionStatus](*transactionOperationFailure(name, sdkgo.FailureTransport, "transaction dispatch did not start"), 0)
	}
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewMutationBranch(CreateTransactionBranchDefect, TransactionStatus{}, transactionOperationFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if exchange.status == http.StatusTooManyRequests || exchange.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationUncertain(TransactionStatus{}, *transactionOperationFailure(name, sdkgo.FailureTransport, "transaction retry checkpoint failed"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[TransactionStatus](*transactionOperationFailure(name, sdkgo.FailureAvailability, "Paddle requested a retry"), exchange.retryAfter)
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewMutationRetry[TransactionStatus](*transactionOperationFailure(name, sdkgo.FailureAvailability, "transaction creation outcome is unknown"), exchange.retryAfter)
	}
	if exchange.status != http.StatusCreated {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(TransactionStatus{}, *transactionOperationFailure(name, sdkgo.FailureTransport, "an earlier transaction create may have succeeded"), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationBranch(CreateTransactionBranchProviderRejected, TransactionStatus{}, transactionOperationFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected transaction creation"), sdkgo.Receipt{})
	}
	transaction, decodeErr := decodeTransaction(exchange.body, "")
	if decodeErr != nil || exchange.err != nil || transaction.Status != "draft" && transaction.Status != "ready" {
		return sdkgo.NewMutationUncertain(TransactionStatus{}, *transactionOperationFailure(name, sdkgo.FailureProtocol, "Paddle accepted transaction creation but returned invalid data"), sdkgo.Receipt{})
	}
	_ = fence.RecordProviderID(transaction.ID)
	return sdkgo.NewMutationBranch(CreateTransactionBranchCreated, transaction, nil, sdkgo.Receipt{ProviderObjectID: transaction.ID})
}

func (client *Client) replaceTransaction(call sdkgo.Call, name, transactionID string, payload any, expectedStatus string, successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[TransactionStatus] {
	exchange := client.exchange(call, call.Context, http.MethodPatch, "/transactions/"+url.PathEscape(transactionID), payload)
	if errors.Is(exchange.err, errCredential) {
		return sdkgo.NewMutationBranch(defectBranch, TransactionStatus{}, transactionOperationFailure(name, sdkgo.FailureAuthentication, "Paddle credentials are invalid"), sdkgo.Receipt{})
	}
	if paddleRetryable(exchange) {
		return sdkgo.NewMutationRetry[TransactionStatus](*transactionOperationFailure(name, sdkgo.FailureAvailability, "transaction update is unavailable"), exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(notFoundBranch, TransactionStatus{}, transactionOperationFailure(name, sdkgo.FailureNotFound, "transaction was not found"), sdkgo.Receipt{})
	}
	if exchange.status != http.StatusOK {
		return sdkgo.NewMutationBranch(rejectedBranch, TransactionStatus{}, transactionOperationFailure(name, sdkgo.FailureProviderRejection, "Paddle rejected transaction update"), sdkgo.Receipt{})
	}
	transaction, decodeErr := decodeTransaction(exchange.body, transactionID)
	if decodeErr != nil || exchange.err != nil || expectedStatus != "" && transaction.Status != expectedStatus {
		return sdkgo.NewMutationBranch(invalidBranch, TransactionStatus{}, transactionOperationFailure(name, sdkgo.FailureProtocol, "Paddle returned an invalid transaction"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(successBranch, transaction, nil, sdkgo.Receipt{ProviderObjectID: transaction.ID})
}

// Invoke attaches a customer and billing address to a draft transaction.
func (operation UpdateTransactionOperation) Invoke(call sdkgo.Call, input UpdateTransactionInput) sdkgo.MutationAttempt[TransactionStatus] {
	const name = "updateTransaction"
	if !validTransactionID(input.TransactionID) || !validPaddleID(input.CustomerID, "ctm_") || !validPaddleID(input.AddressID, "add_") {
		return sdkgo.NewMutationBranch(UpdateTransactionBranchDefect, TransactionStatus{}, transactionOperationFailure(name, sdkgo.FailureValidation, "transaction, customer, or address ID is invalid"), sdkgo.Receipt{})
	}
	return operation.client.replaceTransaction(call, name, input.TransactionID, map[string]string{"customer_id": input.CustomerID, "address_id": input.AddressID}, "", UpdateTransactionBranchUpdated, UpdateTransactionBranchNotFound, UpdateTransactionBranchProviderRejected, UpdateTransactionBranchInvalidResponse, UpdateTransactionBranchDefect)
}

// Invoke cancels a transaction with an absolute status replacement.
func (operation CancelTransactionOperation) Invoke(call sdkgo.Call, input GetTransactionInput) sdkgo.MutationAttempt[TransactionStatus] {
	const name = "cancelTransaction"
	if !validTransactionID(input.TransactionID) {
		return sdkgo.NewMutationBranch(CancelTransactionBranchDefect, TransactionStatus{}, transactionOperationFailure(name, sdkgo.FailureValidation, "transaction ID is invalid"), sdkgo.Receipt{})
	}
	return operation.client.replaceTransaction(call, name, input.TransactionID, map[string]string{"status": "canceled"}, "canceled", CancelTransactionBranchCanceled, CancelTransactionBranchNotFound, CancelTransactionBranchProviderRejected, CancelTransactionBranchInvalidResponse, CancelTransactionBranchDefect)
}
