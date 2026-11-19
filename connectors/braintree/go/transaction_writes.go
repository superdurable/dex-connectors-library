// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const chargePaymentMethodGraphQL = "mutation ChargePaymentMethod($input: ChargePaymentMethodInput!) { chargePaymentMethod(input: $input) { transaction { id status } } }"
const captureTransactionGraphQL = "mutation CaptureTransaction($input: CaptureTransactionInput!) { captureTransaction(input: $input) { transaction { id status } } }"
const voidTransactionGraphQL = "mutation VoidTransaction($input: VoidTransactionInput!) { voidTransaction(input: $input) { transaction { id status } } }"
const refundTransactionGraphQL = "mutation RefundTransaction($input: RefundTransactionInput!) { refundTransaction(input: $input) { refund { id status } } }"

// ChargePaymentMethodInput supplies a tokenized payment method and charge amount.
type ChargePaymentMethodInput struct {
	// PaymentMethodID is a single-use or vaulted GraphQL payment method ID.
	PaymentMethodID string `json:"paymentMethodId"`
	// Amount is a positive decimal amount in the merchant account's currency.
	Amount string `json:"amount"`
	// MerchantAccountID selects a merchant account; blank uses the provider default.
	MerchantAccountID string `json:"merchantAccountId,omitempty"`
	// OrderID is an optional merchant reconciliation ID.
	OrderID string `json:"orderId,omitempty"`
}

// CaptureTransactionInput identifies an authorization and optional partial amount.
type CaptureTransactionInput struct {
	// TransactionID is the authorized GraphQL transaction ID.
	TransactionID string `json:"transactionId"`
	// Amount is a positive partial amount; blank captures the entire authorization.
	Amount string `json:"amount,omitempty"`
}

// RefundTransactionInput identifies a settled transaction and optional partial amount.
type RefundTransactionInput struct {
	// TransactionID is the settled GraphQL transaction ID.
	TransactionID string `json:"transactionId"`
	// Amount is a positive partial refund; blank refunds the entire transaction.
	Amount string `json:"amount,omitempty"`
	// OrderID is an optional refund reconciliation ID; blank inherits the transaction's.
	OrderID string `json:"orderId,omitempty"`
}

// ChargePaymentMethodOperation applies chargePaymentMethod with a provider deduplication key.
type ChargePaymentMethodOperation struct{ client *Client }

// ChargePaymentMethod binds the financial Mutation.
func (client *Client) ChargePaymentMethod() ChargePaymentMethodOperation {
	return ChargePaymentMethodOperation{client}
}

// Definition returns the generated Mutation contract.
func (ChargePaymentMethodOperation) Definition() sdkgo.MutationDefinition {
	return ChargePaymentMethodDefinition
}

// IdempotencyKey supplies the stable Braintree apiRequestKey for this Dex call.
func (ChargePaymentMethodOperation) IdempotencyKey(callID sdkgo.CallID, _ ChargePaymentMethodInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies chargePaymentMethod with repeat-safe provider deduplication.
func (operation ChargePaymentMethodOperation) Invoke(call sdkgo.Call, input ChargePaymentMethodInput) sdkgo.MutationAttempt[TransactionProfile] {
	if !(validGraphQLID(input.PaymentMethodID) && validBraintreeAmount(input.Amount) && (input.MerchantAccountID == "" || validGraphQLID(input.MerchantAccountID)) && validOrderID(input.OrderID)) {
		return braintreeMutationBranch[TransactionProfile]("chargePaymentMethod", ChargePaymentMethodBranchDefect, sdkgo.FailureValidation, "financial mutation input is invalid")
	}
	transaction := map[string]any{"amount": input.Amount}
	if input.MerchantAccountID != "" {
		transaction["merchantAccountId"] = input.MerchantAccountID
	}
	if input.OrderID != "" {
		transaction["orderId"] = input.OrderID
	}
	requestInput := map[string]any{"paymentMethodId": input.PaymentMethodID, "transaction": transaction}
	requestInput["clientMutationId"] = string(call.ID)
	requestInput["apiRequestKey"] = string(call.IdempotencyKey)
	response := operation.client.graphQL(call, call.Context, chargePaymentMethodGraphQL, map[string]any{"input": requestInput})
	return financialMutationAttempt[TransactionProfile](call, response, "chargePaymentMethod", "transaction", "", ChargePaymentMethodBranchCharged, ChargePaymentMethodBranchNotFound, ChargePaymentMethodBranchProviderRejected, ChargePaymentMethodBranchDefect)
}

// CaptureTransactionOperation applies captureTransaction with a provider deduplication key.
type CaptureTransactionOperation struct{ client *Client }

// CaptureTransaction binds the financial Mutation.
func (client *Client) CaptureTransaction() CaptureTransactionOperation {
	return CaptureTransactionOperation{client}
}

// Definition returns the generated Mutation contract.
func (CaptureTransactionOperation) Definition() sdkgo.MutationDefinition {
	return CaptureTransactionDefinition
}

// IdempotencyKey supplies the stable Braintree apiRequestKey for this Dex call.
func (CaptureTransactionOperation) IdempotencyKey(callID sdkgo.CallID, _ CaptureTransactionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies captureTransaction with repeat-safe provider deduplication.
func (operation CaptureTransactionOperation) Invoke(call sdkgo.Call, input CaptureTransactionInput) sdkgo.MutationAttempt[TransactionProfile] {
	if !(validGraphQLID(input.TransactionID) && (input.Amount == "" || validBraintreeAmount(input.Amount))) {
		return braintreeMutationBranch[TransactionProfile]("captureTransaction", CaptureTransactionBranchDefect, sdkgo.FailureValidation, "financial mutation input is invalid")
	}
	requestInput := map[string]any{"transactionId": input.TransactionID}
	if input.Amount != "" {
		requestInput["transaction"] = map[string]any{"amount": input.Amount}
	}
	requestInput["clientMutationId"] = string(call.ID)
	requestInput["apiRequestKey"] = string(call.IdempotencyKey)
	response := operation.client.graphQL(call, call.Context, captureTransactionGraphQL, map[string]any{"input": requestInput})
	return financialMutationAttempt[TransactionProfile](call, response, "captureTransaction", "transaction", input.TransactionID, CaptureTransactionBranchCaptured, CaptureTransactionBranchNotFound, CaptureTransactionBranchProviderRejected, CaptureTransactionBranchDefect)
}

// VoidTransactionOperation applies voidTransaction with a provider deduplication key.
type VoidTransactionOperation struct{ client *Client }

// VoidTransaction binds the financial Mutation.
func (client *Client) VoidTransaction() VoidTransactionOperation {
	return VoidTransactionOperation{client}
}

// Definition returns the generated Mutation contract.
func (VoidTransactionOperation) Definition() sdkgo.MutationDefinition {
	return VoidTransactionDefinition
}

// IdempotencyKey supplies the stable Braintree apiRequestKey for this Dex call.
func (VoidTransactionOperation) IdempotencyKey(callID sdkgo.CallID, _ GetTransactionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies voidTransaction with repeat-safe provider deduplication.
func (operation VoidTransactionOperation) Invoke(call sdkgo.Call, input GetTransactionInput) sdkgo.MutationAttempt[TransactionProfile] {
	if !(validGraphQLID(input.TransactionID)) {
		return braintreeMutationBranch[TransactionProfile]("voidTransaction", VoidTransactionBranchDefect, sdkgo.FailureValidation, "financial mutation input is invalid")
	}

	requestInput := map[string]any{"transactionId": input.TransactionID}
	requestInput["clientMutationId"] = string(call.ID)
	requestInput["apiRequestKey"] = string(call.IdempotencyKey)
	response := operation.client.graphQL(call, call.Context, voidTransactionGraphQL, map[string]any{"input": requestInput})
	return financialMutationAttempt[TransactionProfile](call, response, "voidTransaction", "transaction", input.TransactionID, VoidTransactionBranchVoided, VoidTransactionBranchNotFound, VoidTransactionBranchProviderRejected, VoidTransactionBranchDefect)
}

// RefundTransactionOperation applies refundTransaction with a provider deduplication key.
type RefundTransactionOperation struct{ client *Client }

// RefundTransaction binds the financial Mutation.
func (client *Client) RefundTransaction() RefundTransactionOperation {
	return RefundTransactionOperation{client}
}

// Definition returns the generated Mutation contract.
func (RefundTransactionOperation) Definition() sdkgo.MutationDefinition {
	return RefundTransactionDefinition
}

// IdempotencyKey supplies the stable Braintree apiRequestKey for this Dex call.
func (RefundTransactionOperation) IdempotencyKey(callID sdkgo.CallID, _ RefundTransactionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke applies refundTransaction with repeat-safe provider deduplication.
func (operation RefundTransactionOperation) Invoke(call sdkgo.Call, input RefundTransactionInput) sdkgo.MutationAttempt[RefundProfile] {
	if !(validGraphQLID(input.TransactionID) && (input.Amount == "" || validBraintreeAmount(input.Amount)) && validOrderID(input.OrderID)) {
		return braintreeMutationBranch[RefundProfile]("refundTransaction", RefundTransactionBranchDefect, sdkgo.FailureValidation, "financial mutation input is invalid")
	}
	requestInput := map[string]any{"transactionId": input.TransactionID}
	refund := map[string]any{}
	if input.Amount != "" {
		refund["amount"] = input.Amount
	}
	if input.OrderID != "" {
		refund["orderId"] = input.OrderID
	}
	if len(refund) > 0 {
		requestInput["refund"] = refund
	}
	requestInput["clientMutationId"] = string(call.ID)
	requestInput["apiRequestKey"] = string(call.IdempotencyKey)
	response := operation.client.graphQL(call, call.Context, refundTransactionGraphQL, map[string]any{"input": requestInput})
	return financialMutationAttempt[RefundProfile](call, response, "refundTransaction", "refund", "", RefundTransactionBranchRefunded, RefundTransactionBranchNotFound, RefundTransactionBranchProviderRejected, RefundTransactionBranchDefect)
}

func financialMutationAttempt[T any](call sdkgo.Call, response providerResponse, operation, resource, expectedID string, success, notFound, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[T] {
	if errors.Is(response.err, errCredential) {
		return braintreeMutationBranch[T](operation, defect, sdkgo.FailureAuthentication, "Braintree credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) || response.err != nil && !errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return braintreeMutationRetry[T](operation, sdkgo.FailureTransport, "Braintree financial mutation outcome is unknown", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return braintreeMutationRetry[T](operation, sdkgo.FailureRateLimit, "Braintree rate limited the mutation", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return braintreeMutationRetry[T](operation, sdkgo.FailureAvailability, "Braintree financial mutation outcome is unknown", response.retryAfter)
	}
	if response.status != http.StatusOK {
		if call.Context.Attempt() > 1 && response.status == http.StatusBadRequest {
			return braintreeMutationRetry[T](operation, sdkgo.FailureAvailability, "Braintree idempotency request may still be in flight", response.retryAfter)
		}
		return braintreeMutationBranch[T](operation, rejected, sdkgo.FailureProviderRejection, "Braintree rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return braintreeMutationRetry[T](operation, sdkgo.FailureResponseTooLarge, "Braintree financial mutation response is uncertain", 0)
	}
	var wire struct {
		Data   map[string]map[string]json.RawMessage `json:"data"`
		Errors []json.RawMessage                     `json:"errors"`
	}
	if json.Unmarshal(response.body, &wire) != nil {
		return braintreeMutationRetry[T](operation, sdkgo.FailureProtocol, "Braintree financial mutation response is uncertain", 0)
	}
	if len(wire.Errors) > 0 {
		switch classifyGraphQLErrors(wire.Errors) {
		case graphQLErrorNotFound:
			return braintreeMutationBranch[T](operation, notFound, sdkgo.FailureNotFound, "Braintree did not find the payment resource")
		case graphQLErrorRateLimited:
			return braintreeMutationRetry[T](operation, sdkgo.FailureRateLimit, "Braintree reported a resource limit", response.retryAfter)
		case graphQLErrorUnavailable:
			return braintreeMutationRetry[T](operation, sdkgo.FailureAvailability, "Braintree reported a temporary failure", response.retryAfter)
		default:
			if call.Context.Attempt() > 1 && hasGraphQLErrorClass(wire.Errors, "VALIDATION") {
				return braintreeMutationRetry[T](operation, sdkgo.FailureAvailability, "Braintree idempotency request may still be in flight", 0)
			}
			return braintreeMutationBranch[T](operation, rejected, sdkgo.FailureProviderRejection, "Braintree rejected the mutation")
		}
	}
	var identity struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	raw := wire.Data[operation][resource]
	if json.Unmarshal(raw, &identity) != nil || !validGraphQLID(identity.ID) || identity.Status == "" || (expectedID != "" && identity.ID != expectedID) {
		return braintreeMutationRetry[T](operation, sdkgo.FailureProtocol, "Braintree financial mutation response is uncertain", 0)
	}
	var value T
	if json.Unmarshal(raw, &value) != nil {
		return braintreeMutationRetry[T](operation, sdkgo.FailureProtocol, "Braintree financial mutation response is uncertain", 0)
	}
	return sdkgo.NewMutationBranch(success, value, nil, sdkgo.Receipt{})
}

func hasGraphQLErrorClass(rawErrors []json.RawMessage, class string) bool {
	for _, rawError := range rawErrors {
		var value struct {
			Extensions struct {
				ErrorClass string `json:"errorClass"`
			} `json:"extensions"`
		}
		if json.Unmarshal(rawError, &value) == nil && value.Extensions.ErrorClass == class {
			return true
		}
	}
	return false
}

func validBraintreeAmount(value string) bool {
	if value == "" || len(value) > 14 {
		return false
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || len(parts[0]) == 0 || len(parts[0]) > 10 || len(parts) == 2 && len(parts[1]) != 2 && len(parts[1]) != 3 {
		return false
	}
	hasNonzero := false
	for _, part := range parts {
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return false
			}
			if digit != '0' {
				hasNonzero = true
			}
		}
	}
	return hasNonzero
}

func validOrderID(value string) bool {
	return len(value) <= 255 && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n")
}
