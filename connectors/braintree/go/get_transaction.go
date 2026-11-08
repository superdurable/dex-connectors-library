// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetTransactionInput selects one Braintree GraphQL node by ID.
type GetTransactionInput struct {
	// TransactionID is the GraphQL transaction ID, not a legacy SDK ID.
	TransactionID string `json:"transactionId"`
}

// TransactionProfile contains a transaction's ID and current payment status.
type TransactionProfile struct {
	// ID is the Braintree GraphQL transaction ID.
	ID string `json:"id"`
	// Status is the Braintree PaymentStatus enum value.
	Status string `json:"status"`
}

// GetTransactionOperation reads one transaction through the node query.
type GetTransactionOperation struct{ client *Client }

// GetTransaction binds the Query to this client.
func (client *Client) GetTransaction() GetTransactionOperation {
	return GetTransactionOperation{client}
}

// Definition returns the manifest-generated Query worker.
func (GetTransactionOperation) Definition() sdkgo.QueryDefinition { return GetTransactionDefinition }

// Invoke sends the read-only query and classifies its result.
func (operation GetTransactionOperation) Invoke(call sdkgo.Call, input GetTransactionInput) sdkgo.QueryAttempt[TransactionProfile] {
	if !validGraphQLID(input.TransactionID) {
		return transactionBranch(GetTransactionBranchDefect, sdkgo.FailureValidation, "transactionId must contain 1–128 printable characters")
	}
	response := operation.client.get(call, input)
	if errors.Is(response.err, errCredential) {
		return transactionBranch(GetTransactionBranchDefect, sdkgo.FailureAuthentication, "Braintree credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[TransactionProfile](transactionFailure(sdkgo.FailureTransport, "Braintree request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[TransactionProfile](transactionFailure(sdkgo.FailureRateLimit, "Braintree rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[TransactionProfile](transactionFailure(sdkgo.FailureAvailability, "Braintree is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return transactionBranch(GetTransactionBranchProviderRejected, sdkgo.FailureProviderRejection, "Braintree rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return transactionBranch(GetTransactionBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Braintree response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[TransactionProfile](transactionFailure(sdkgo.FailureTransport, "Braintree response was interrupted"), 0)
	}
	profile, isNotFound, isRejected, err := decodeTransaction(response.body, input.TransactionID)
	if err != nil {
		return transactionBranch(GetTransactionBranchInvalidResponse, sdkgo.FailureProtocol, "Braintree returned invalid transaction JSON")
	}
	if isRejected {
		return transactionBranch(GetTransactionBranchProviderRejected, sdkgo.FailureProviderRejection, "Braintree rejected the GraphQL query")
	}
	if isNotFound {
		return transactionBranch(GetTransactionBranchNotFound, sdkgo.FailureNotFound, "Braintree did not expose the transaction")
	}
	return sdkgo.NewQueryBranch(GetTransactionBranchFound, profile, nil, sdkgo.Receipt{})
}

func validGraphQLID(value string) bool {
	if len(value) == 0 || len(value) > 128 || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func decodeTransaction(body []byte, transactionID string) (TransactionProfile, bool, bool, error) {
	var wire struct {
		Data *struct {
			Node *struct {
				Typename string `json:"__typename"`
				ID       string `json:"id"`
				Status   string `json:"status"`
			} `json:"node"`
		} `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TransactionProfile{}, false, false, err
	}
	if len(wire.Errors) > 0 {
		return TransactionProfile{}, false, true, nil
	}
	if wire.Data == nil {
		return TransactionProfile{}, false, false, errors.New("GraphQL data is missing")
	}
	if wire.Data.Node == nil {
		return TransactionProfile{}, true, false, nil
	}
	if wire.Data.Node.Typename == "" {
		return TransactionProfile{}, false, false, errors.New("GraphQL node type is missing")
	}
	if wire.Data.Node.Typename != "Transaction" {
		return TransactionProfile{}, true, false, nil
	}
	if wire.Data.Node.ID != transactionID || wire.Data.Node.Status == "" {
		return TransactionProfile{}, false, false, errors.New("transaction identity or status is invalid")
	}
	return TransactionProfile{ID: wire.Data.Node.ID, Status: wire.Data.Node.Status}, false, false, nil
}

func transactionFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "braintree", Operation: "getTransaction", Message: message}
}
func transactionBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[TransactionProfile] {
	failure := transactionFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, TransactionProfile{}, &failure, sdkgo.Receipt{})
}
