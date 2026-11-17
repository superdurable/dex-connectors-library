// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package adyen

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var paymentPspReferencePattern = regexp.MustCompile(`^[A-Za-z0-9]{16,64}$`)

// PaymentModificationInput identifies a payment and optional amount to modify.
type PaymentModificationInput struct {
	// MerchantAccount is the account in Customer Area > Settings > Merchant accounts.
	MerchantAccount string `json:"merchantAccount"`
	// PaymentPspReference is the payment reference from Customer Area > Payments > Payment list.
	PaymentPspReference string `json:"paymentPspReference"`
	// Reference is an optional merchant reconciliation reference of at most 80 characters.
	Reference string `json:"reference,omitempty"`
	// CurrencyCode is the original payment's three-letter ISO currency for amount operations.
	CurrencyCode string `json:"currencyCode,omitempty"`
	// AmountMinorUnits is the positive capture/refund amount or replacement authorised amount.
	AmountMinorUnits int64 `json:"amountMinorUnits,omitempty"`
}

// PaymentReferenceModificationInput selects a payment for a full cancel or reversal.
type PaymentReferenceModificationInput struct {
	// MerchantAccount is the account in Customer Area > Settings > Merchant accounts.
	MerchantAccount string `json:"merchantAccount"`
	// PaymentPspReference is the payment reference from Customer Area > Payments > Payment list.
	PaymentPspReference string `json:"paymentPspReference"`
	// Reference is an optional merchant reconciliation reference of at most 80 characters.
	Reference string `json:"reference,omitempty"`
}

// PaymentModificationReceipt confirms Adyen accepted a modification request, not its final outcome.
type PaymentModificationReceipt struct {
	// PaymentPspReference identifies the payment being modified.
	PaymentPspReference string `json:"paymentPspReference"`
	// ModificationPspReference identifies this asynchronous modification request.
	ModificationPspReference string `json:"modificationPspReference"`
	// Status is received; a later webhook determines the final outcome.
	Status string `json:"status"`
}

// CapturePaymentOperation requests capture of an authorised amount.
type CapturePaymentOperation struct{ client *Client }

// CancelPaymentOperation requests cancellation before capture.
type CancelPaymentOperation struct{ client *Client }

// RefundPaymentOperation requests a captured-amount refund.
type RefundPaymentOperation struct{ client *Client }

// ReversePaymentOperation requests a full cancel-or-refund.
type ReversePaymentOperation struct{ client *Client }

// UpdateAuthorisedAmountOperation requests an absolute authorisation amount.
type UpdateAuthorisedAmountOperation struct{ client *Client }

// CapturePayment binds the capture Mutation.
func (client *Client) CapturePayment() CapturePaymentOperation {
	return CapturePaymentOperation{client}
}

// CancelPayment binds the cancel Mutation.
func (client *Client) CancelPayment() CancelPaymentOperation { return CancelPaymentOperation{client} }

// RefundPayment binds the refund Mutation.
func (client *Client) RefundPayment() RefundPaymentOperation { return RefundPaymentOperation{client} }

// ReversePayment binds the full reversal Mutation.
func (client *Client) ReversePayment() ReversePaymentOperation {
	return ReversePaymentOperation{client}
}

// UpdateAuthorisedAmount binds the amount-update Mutation.
func (client *Client) UpdateAuthorisedAmount() UpdateAuthorisedAmountOperation {
	return UpdateAuthorisedAmountOperation{client}
}

// Definition returns the generated capture contract.
func (CapturePaymentOperation) Definition() sdkgo.MutationDefinition { return CapturePaymentDefinition }

// Definition returns the generated cancel contract.
func (CancelPaymentOperation) Definition() sdkgo.MutationDefinition { return CancelPaymentDefinition }

// Definition returns the generated refund contract.
func (RefundPaymentOperation) Definition() sdkgo.MutationDefinition { return RefundPaymentDefinition }

// Definition returns the generated reversal contract.
func (ReversePaymentOperation) Definition() sdkgo.MutationDefinition { return ReversePaymentDefinition }

// Definition returns the generated amount-update contract.
func (UpdateAuthorisedAmountOperation) Definition() sdkgo.MutationDefinition {
	return UpdateAuthorisedAmountDefinition
}

// IdempotencyKey supplies Adyen the capture's stable key across retries.
func (CapturePaymentOperation) IdempotencyKey(id sdkgo.CallID, _ PaymentModificationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey supplies Adyen the cancel's stable key across retries.
func (CancelPaymentOperation) IdempotencyKey(id sdkgo.CallID, _ PaymentReferenceModificationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey supplies Adyen the refund's stable key across retries.
func (RefundPaymentOperation) IdempotencyKey(id sdkgo.CallID, _ PaymentModificationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey supplies Adyen the reversal's stable key across retries.
func (ReversePaymentOperation) IdempotencyKey(id sdkgo.CallID, _ PaymentReferenceModificationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// IdempotencyKey supplies Adyen the amount update's stable key across retries.
func (UpdateAuthorisedAmountOperation) IdempotencyKey(id sdkgo.CallID, _ PaymentModificationInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

// Invoke sends one idempotent capture request.
func (operation CapturePaymentOperation) Invoke(call sdkgo.Call, input PaymentModificationInput) sdkgo.MutationAttempt[PaymentModificationReceipt] {
	return operation.client.invokePaymentModification(call, input, "capturePayment", "captures", true, CapturePaymentBranchReceived, CapturePaymentBranchProviderRejected, CapturePaymentBranchDefect)
}

// Invoke sends one idempotent cancellation request.
func (operation CancelPaymentOperation) Invoke(call sdkgo.Call, input PaymentReferenceModificationInput) sdkgo.MutationAttempt[PaymentModificationReceipt] {
	return operation.client.invokePaymentModification(call, PaymentModificationInput{MerchantAccount: input.MerchantAccount, PaymentPspReference: input.PaymentPspReference, Reference: input.Reference}, "cancelPayment", "cancels", false, CancelPaymentBranchReceived, CancelPaymentBranchProviderRejected, CancelPaymentBranchDefect)
}

// Invoke sends one idempotent refund request.
func (operation RefundPaymentOperation) Invoke(call sdkgo.Call, input PaymentModificationInput) sdkgo.MutationAttempt[PaymentModificationReceipt] {
	return operation.client.invokePaymentModification(call, input, "refundPayment", "refunds", true, RefundPaymentBranchReceived, RefundPaymentBranchProviderRejected, RefundPaymentBranchDefect)
}

// Invoke sends one idempotent full reversal request.
func (operation ReversePaymentOperation) Invoke(call sdkgo.Call, input PaymentReferenceModificationInput) sdkgo.MutationAttempt[PaymentModificationReceipt] {
	return operation.client.invokePaymentModification(call, PaymentModificationInput{MerchantAccount: input.MerchantAccount, PaymentPspReference: input.PaymentPspReference, Reference: input.Reference}, "reversePayment", "reversals", false, ReversePaymentBranchReceived, ReversePaymentBranchProviderRejected, ReversePaymentBranchDefect)
}

// Invoke sends one idempotent absolute amount update.
func (operation UpdateAuthorisedAmountOperation) Invoke(call sdkgo.Call, input PaymentModificationInput) sdkgo.MutationAttempt[PaymentModificationReceipt] {
	return operation.client.invokePaymentModification(call, input, "updateAuthorisedAmount", "amountUpdates", true, UpdateAuthorisedAmountBranchReceived, UpdateAuthorisedAmountBranchProviderRejected, UpdateAuthorisedAmountBranchDefect)
}

func (client *Client) invokePaymentModification(call sdkgo.Call, input PaymentModificationInput, operation, pathSuffix string, hasAmount bool, received, rejected, defect sdkgo.BranchID) sdkgo.MutationAttempt[PaymentModificationReceipt] {
	if !validPaymentModificationInput(input, hasAmount) || len(call.IdempotencyKey) == 0 || len(call.IdempotencyKey) > 64 {
		return paymentModificationBranch(operation, defect, sdkgo.FailureValidation, "payment modification fields or idempotency key are invalid")
	}
	request := map[string]any{"merchantAccount": input.MerchantAccount}
	if input.Reference != "" {
		request["reference"] = input.Reference
	}
	if hasAmount {
		request["amount"] = map[string]any{"currency": input.CurrencyCode, "value": input.AmountMinorUnits}
	}
	requestBody, err := json.Marshal(request)
	if err != nil {
		return paymentModificationBranch(operation, defect, sdkgo.FailureLocalDefect, "modification request could not be encoded")
	}
	response := client.post(call, "/payments/"+input.PaymentPspReference+"/"+pathSuffix, requestBody)
	if errors.Is(response.err, errCredential) {
		return paymentModificationBranch(operation, defect, sdkgo.FailureAuthentication, "Adyen credentials unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return paymentModificationRetry(operation, sdkgo.FailureTransport, "modification request outcome unknown", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return paymentModificationRetry(operation, sdkgo.FailureRateLimit, "Adyen rate limited the modification", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return paymentModificationRetry(operation, sdkgo.FailureAvailability, "modification request outcome unknown", response.retryAfter)
	}
	if response.status != http.StatusCreated {
		return paymentModificationBranch(operation, rejected, sdkgo.FailureProviderRejection, "Adyen rejected HTTP "+strconv.Itoa(response.status))
	}
	// The modification may have been accepted even when the response is unreadable.
	if response.err != nil {
		return paymentModificationRetry(operation, sdkgo.FailureTransport, "modification response interrupted", 0)
	}
	var wire struct {
		MerchantAccount     string `json:"merchantAccount"`
		PaymentPspReference string `json:"paymentPspReference"`
		PspReference        string `json:"pspReference"`
		Status              string `json:"status"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.MerchantAccount != input.MerchantAccount || wire.PaymentPspReference != input.PaymentPspReference || !paymentPspReferencePattern.MatchString(wire.PspReference) || wire.Status != "received" {
		return paymentModificationRetry(operation, sdkgo.FailureProtocol, "modification response invalid; outcome unknown", 0)
	}
	result := PaymentModificationReceipt{PaymentPspReference: wire.PaymentPspReference, ModificationPspReference: wire.PspReference, Status: wire.Status}
	receipt := sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "adyen", ProviderObjectID: wire.PspReference, ObservedAt: time.Now().UTC()}
	return sdkgo.NewMutationBranch(received, result, nil, receipt)
}

func validPaymentModificationInput(input PaymentModificationInput, hasAmount bool) bool {
	if strings.TrimSpace(input.MerchantAccount) != input.MerchantAccount || input.MerchantAccount == "" || len(input.MerchantAccount) > 128 || strings.ContainsAny(input.MerchantAccount, "\r\n") || !paymentPspReferencePattern.MatchString(input.PaymentPspReference) || len(input.Reference) > 80 || strings.TrimSpace(input.Reference) != input.Reference || strings.ContainsAny(input.Reference, "\r\n") {
		return false
	}
	if hasAmount {
		if input.AmountMinorUnits <= 0 || len(input.CurrencyCode) != 3 {
			return false
		}
		for _, character := range input.CurrencyCode {
			if character < 'A' || character > 'Z' {
				return false
			}
		}
	} else if input.AmountMinorUnits != 0 || input.CurrencyCode != "" {
		return false
	}
	return true
}

func paymentModificationRetry(operation string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[PaymentModificationReceipt] {
	return sdkgo.NewMutationRetry[PaymentModificationReceipt](sdkgo.Failure{Kind: kind, Provider: "adyen", Operation: operation, Message: message}, delay)
}

func paymentModificationBranch(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PaymentModificationReceipt] {
	failure := sdkgo.Failure{Kind: kind, Provider: "adyen", Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(branch, PaymentModificationReceipt{}, &failure, sdkgo.Receipt{})
}
