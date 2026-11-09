// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paddle

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetTransactionInput selects a transaction by its non-secret Paddle ID.
type GetTransactionInput struct {
	// TransactionID is a Paddle Billing transaction ID prefixed txn_.
	TransactionID string `json:"transactionId"`
}

// TransactionStatus excludes customer identities and payment details.
type TransactionStatus struct {
	// ID is the Paddle transaction ID.
	ID string `json:"id"`
	// Status is a Paddle transaction state such as completed.
	Status string `json:"status"`
	// CurrencyCode is the transaction's ISO 4217 currency.
	CurrencyCode string `json:"currencyCode"`
}

// GetTransactionOperation reads one transaction's identity and state.
type GetTransactionOperation struct{ client *Client }

// GetTransaction binds the Query to this client.
func (client *Client) GetTransaction() GetTransactionOperation {
	return GetTransactionOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetTransactionOperation) Definition() sdkgo.QueryDefinition { return GetTransactionDefinition }

// Invoke reads one transaction and classifies retryable and definitive outcomes.
func (operation GetTransactionOperation) Invoke(call sdkgo.Call, input GetTransactionInput) sdkgo.QueryAttempt[TransactionStatus] {
	if !validTransactionID(input.TransactionID) {
		return transactionBranch(GetTransactionBranchDefect, sdkgo.FailureValidation, "transaction ID must start with txn_ and contain 26 lowercase letters or digits")
	}
	response := operation.client.get(call, "/transactions/"+url.PathEscape(input.TransactionID))
	if errors.Is(response.err, errCredential) {
		return transactionBranch(GetTransactionBranchDefect, sdkgo.FailureAuthentication, "Paddle credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[TransactionStatus](transactionFailure(sdkgo.FailureTransport, "Paddle request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[TransactionStatus](transactionFailure(sdkgo.FailureRateLimit, "Paddle rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[TransactionStatus](transactionFailure(sdkgo.FailureAvailability, "Paddle is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return transactionBranch(GetTransactionBranchNotFound, sdkgo.FailureNotFound, "Paddle did not expose the transaction")
	}
	if response.status != http.StatusOK {
		return transactionBranch(GetTransactionBranchProviderRejected, sdkgo.FailureProviderRejection, "Paddle rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return transactionBranch(GetTransactionBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Paddle response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[TransactionStatus](transactionFailure(sdkgo.FailureTransport, "Paddle response was interrupted"), 0)
	}
	transactionStatus, err := decodeTransaction(response.body, input.TransactionID)
	if err != nil {
		return transactionBranch(GetTransactionBranchInvalidResponse, sdkgo.FailureProtocol, "Paddle returned invalid transaction JSON")
	}
	return sdkgo.NewQueryBranch(GetTransactionBranchFound, transactionStatus, nil, sdkgo.Receipt{})
}

func validTransactionID(transactionID string) bool {
	if len(transactionID) != 30 || !strings.HasPrefix(transactionID, "txn_") {
		return false
	}
	for _, character := range transactionID[4:] {
		if character < 'a' || character > 'z' {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func decodeTransaction(body []byte, requestedTransactionID string) (TransactionStatus, error) {
	var wire struct {
		Data struct {
			ID           string `json:"id"`
			Status       string `json:"status"`
			CurrencyCode string `json:"currency_code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TransactionStatus{}, err
	}
	if wire.Data.ID != requestedTransactionID || strings.TrimSpace(wire.Data.Status) == "" || len(wire.Data.CurrencyCode) != 3 {
		return TransactionStatus{}, fmt.Errorf("transaction identity, status, or currency missing")
	}
	return TransactionStatus{ID: wire.Data.ID, Status: wire.Data.Status, CurrencyCode: wire.Data.CurrencyCode}, nil
}

func transactionFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "paddle", Operation: "getTransaction", Message: message}
}

func transactionBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TransactionStatus] {
	failure := transactionFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, TransactionStatus{}, &failure, sdkgo.Receipt{})
}
