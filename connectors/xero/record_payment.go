// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const recordPaymentOperationID = "recordPayment"

// paymentAmountRules allows cents only, because Xero applies payments in the invoice's currency to two places.
var paymentAmountRules = decimalRules{maximumFractionDigits: 2, maximumIntegerDigits: 12}

// RecordPaymentInput is one payment against an approved invoice.
type RecordPaymentInput struct {
	// InvoiceID is the Xero UUID of an AUTHORISED invoice or bill.
	InvoiceID string `json:"invoiceId"`
	// AccountID is the Xero UUID of a BANK account, or of an account with payments enabled.
	// Set exactly one of AccountID and AccountCode.
	AccountID string `json:"accountId,omitempty"`
	// AccountCode is that account's code, such as 090, for accounts that have one.
	AccountCode string `json:"accountCode,omitempty"`
	// Date is the payment date, YYYY-MM-DD.
	Date string `json:"date"`
	// Amount is the payment in the invoice's currency, with at most two decimal places; it must
	// not exceed the invoice's amount due.
	Amount Decimal `json:"amount"`
	// BankAmount is the amount in the account's currency for a multicurrency payment, which Xero
	// prefers to a rate; blank lets Xero apply its rate.
	BankAmount Decimal `json:"bankAmount,omitempty"`
	// Reference is an optional description, such as the card processor's charge ID.
	Reference string `json:"reference,omitempty"`
	// IsReconciled marks the payment reconciled without a bank statement line, for conversions.
	IsReconciled bool `json:"isReconciled,omitempty"`
}

// RecordPaymentOutput is the recorded payment. On every other branch Payment is empty and
// InvoiceID echoes the request, so an application can read the invoice back.
type RecordPaymentOutput struct {
	// Payment is the payment Xero recorded or, for a replayed attempt, recorded earlier for this Step.
	Payment Payment `json:"payment"`
	// InvoiceID echoes the paid invoice.
	InvoiceID string `json:"invoiceId"`
	// WarningCount is the number of validation warnings Xero attached to the accepted payment;
	// their text is not exposed.
	WarningCount int `json:"warningCount,omitempty"`
}

// RecordPaymentOperation implements the recordPayment Mutation.
type RecordPaymentOperation struct{ client *Client }

type recordPaymentRequestWire struct {
	Invoice      invoiceReferenceWire `json:"Invoice"`
	Account      accountReferenceWire `json:"Account"`
	Date         string               `json:"Date"`
	Amount       json.Number          `json:"Amount"`
	BankAmount   json.Number          `json:"BankAmount,omitempty"`
	Reference    string               `json:"Reference,omitempty"`
	IsReconciled bool                 `json:"IsReconciled,omitempty"`
}

type invoiceReferenceWire struct {
	InvoiceID string `json:"InvoiceID"`
}

type accountReferenceWire struct {
	AccountID string `json:"AccountID,omitempty"`
	Code      string `json:"Code,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (RecordPaymentOperation) Definition() sdkgo.MutationDefinition { return RecordPaymentDefinition }

// IdempotencyKey is the stable Call ID, sent as Xero's Idempotency-Key header. Every attempt of one
// Step execution, including one on a replacement Worker, sends the same key.
func (RecordPaymentOperation) IdempotencyKey(callID sdkgo.CallID, _ RecordPaymentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke records the payment with PUT /Payments under the call's Idempotency-Key, so a retried or
// re-dispatched attempt receives Xero's cached payment instead of paying the invoice twice.
func (operation RecordPaymentOperation) Invoke(call sdkgo.Call, input RecordPaymentInput) sdkgo.MutationAttempt[RecordPaymentOutput] {
	client := operation.client
	requested := RecordPaymentOutput{InvoiceID: strings.ToLower(strings.TrimSpace(input.InvoiceID))}
	payload, err := buildRecordPaymentRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(RecordPaymentBranchDefect, requested, xeroFailurePointer(sdkgo.FailureValidation, recordPaymentOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, recordPaymentOperationID, xeroRequest{method: http.MethodPut, path: "/Payments", payload: payload, isMutation: true})
	receipt := client.receipt(call, result, "")
	if attempt, isTerminal := mutationAttemptForExchange(result, requested, receipt, mutationBranches{
		providerRejected: RecordPaymentBranchProviderRejected, dailyLimitReached: RecordPaymentBranchDailyLimitReached, defect: RecordPaymentBranchDefect,
	}); isTerminal {
		return attempt
	}
	wire, err := decodeSingleResource[paymentWire](result.response.body, "Payments")
	if err == nil && (bool(wire.HasValidationErrors) || len(wire.ValidationErrors) != 0) {
		return sdkgo.NewMutationBranch(RecordPaymentBranchProviderRejected, requested, xeroFailurePointer(sdkgo.FailureValidation, recordPaymentOperationID,
			withErrorSummary("Xero rejected the payment", xeroErrorSummary{validationErrorCount: len(wire.ValidationErrors)})), receipt)
	}
	var payment Payment
	if err == nil {
		payment, err = decodePayment(wire)
	}
	if err == nil && payment.Invoice.InvoiceID != "" && payment.Invoice.InvoiceID != requested.InvoiceID {
		err = errors.New("payment applies to another invoice")
	}
	if err != nil {
		return sdkgo.NewMutationUncertain(requested, xeroFailure(sdkgo.FailureProtocol, recordPaymentOperationID,
			"Xero accepted the payment but returned an unusable payment: "+err.Error()), receipt)
	}
	receipt.ProviderObjectID = payment.PaymentID
	return sdkgo.NewMutationBranch(RecordPaymentBranchRecorded, RecordPaymentOutput{
		Payment: payment, InvoiceID: requested.InvoiceID, WarningCount: len(wire.Warnings),
	}, nil, receipt)
}

// buildRecordPaymentRequest is deterministic, because Xero rejects a repeated key whose request differs.
func buildRecordPaymentRequest(input RecordPaymentInput) (recordPaymentRequestWire, error) {
	invoiceID := strings.ToLower(strings.TrimSpace(input.InvoiceID))
	if !uuidPattern.MatchString(invoiceID) {
		return recordPaymentRequestWire{}, errors.New("invoiceId must be a Xero invoice UUID such as 96df0dff-43ec-4899-a7d9-e9d63ef12b19")
	}
	accountID, accountCode := strings.ToLower(strings.TrimSpace(input.AccountID)), strings.TrimSpace(input.AccountCode)
	switch {
	case (accountID == "") == (accountCode == ""):
		return recordPaymentRequestWire{}, errors.New("set exactly one of accountId and accountCode")
	case accountID != "" && !uuidPattern.MatchString(accountID):
		return recordPaymentRequestWire{}, errors.New("accountId must be a Xero account UUID")
	case accountCode != "" && !accountingCodePattern.MatchString(accountCode):
		return recordPaymentRequestWire{}, errors.New("accountCode must be a Xero account code such as 090")
	}
	date := strings.TrimSpace(input.Date)
	if err := validateCalendarDate("date", date); err != nil {
		return recordPaymentRequestWire{}, err
	}
	if err := validateInputDecimal("amount", input.Amount, paymentAmountRules); err != nil {
		return recordPaymentRequestWire{}, err
	}
	payload := recordPaymentRequestWire{
		Invoice: invoiceReferenceWire{InvoiceID: invoiceID}, Account: accountReferenceWire{AccountID: accountID, Code: accountCode},
		Date: date, Amount: jsonNumberLiteral(input.Amount), Reference: strings.TrimSpace(input.Reference), IsReconciled: input.IsReconciled,
	}
	if input.BankAmount != "" {
		if err := validateInputDecimal("bankAmount", input.BankAmount, paymentAmountRules); err != nil {
			return recordPaymentRequestWire{}, err
		}
		payload.BankAmount = jsonNumberLiteral(input.BankAmount)
	}
	if err := validateSingleLineText("reference", payload.Reference, MaxReferenceCharacters); err != nil {
		return recordPaymentRequestWire{}, err
	}
	return payload, nil
}
