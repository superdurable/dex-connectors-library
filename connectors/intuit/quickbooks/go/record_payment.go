// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	recordPaymentOperationID = "recordPayment"
	// maximumPaymentReferenceCharacters is QuickBooks's PaymentRefNum limit.
	maximumPaymentReferenceCharacters = 21
)

// paymentAmountRules keeps QuickBooks's documented 10.5 line Amount format to whole cents.
var paymentAmountRules = decimalRules{maximumFractionDigits: 2, maximumIntegerDigits: 10}

// RecordPaymentInput describes one received payment applied in full to one invoice.
type RecordPaymentInput struct {
	// CustomerID is the paying customer's Id; it must be the invoice's customer.
	CustomerID string `json:"customerId"`
	// InvoiceID is the paid invoice's Id.
	InvoiceID string `json:"invoiceId"`
	// Amount is the exact amount received and applied to the invoice, with at most two decimal
	// places, such as the invoice's Balance.
	Amount Decimal `json:"amount"`
	// TxnDate is the payment date, YYYY-MM-DD; blank uses today in the company's time zone.
	TxnDate string `json:"txnDate,omitempty"`
	// PaymentReferenceNumber is QuickBooks's PaymentRefNum, such as a check or charge number, at
	// most 21 characters.
	PaymentReferenceNumber string `json:"paymentReferenceNumber,omitempty"`
	// DepositAccountID is the bank or other current asset account's Id; blank deposits to
	// QuickBooks's Undeposited Funds account.
	DepositAccountID string `json:"depositAccountId,omitempty"`
	// PaymentMethodID is the payment method's Id, such as for Credit Card; blank leaves it unset.
	PaymentMethodID string `json:"paymentMethodId,omitempty"`
	// CurrencyCode is the payment's ISO 4217 currency, which must be the customer's; blank uses
	// the customer's currency.
	CurrencyCode string `json:"currencyCode,omitempty"`
	// PrivateNote is an internal memo, at most 4000 characters.
	PrivateNote string `json:"privateNote,omitempty"`
}

// RecordPaymentOperation implements the recordPayment Mutation.
type RecordPaymentOperation struct{ client *Client }

type recordPaymentRequestWire struct {
	CustomerRef         referenceWire          `json:"CustomerRef"`
	TotalAmt            json.Number            `json:"TotalAmt"`
	TxnDate             string                 `json:"TxnDate,omitempty"`
	Line                []paymentLineInputWire `json:"Line"`
	PaymentRefNum       string                 `json:"PaymentRefNum,omitempty"`
	DepositToAccountRef *referenceWire         `json:"DepositToAccountRef,omitempty"`
	PaymentMethodRef    *referenceWire         `json:"PaymentMethodRef,omitempty"`
	CurrencyRef         *referenceWire         `json:"CurrencyRef,omitempty"`
	PrivateNote         string                 `json:"PrivateNote,omitempty"`
}

type paymentLineInputWire struct {
	Amount    json.Number     `json:"Amount"`
	LinkedTxn []linkedTxnWire `json:"LinkedTxn"`
}

// Definition returns the immutable connector operation definition.
func (RecordPaymentOperation) Definition() sdkgo.MutationDefinition { return RecordPaymentDefinition }

// IdempotencyKey is the stable Call ID, sent as QuickBooks's requestid. Every attempt of one Step
// execution, including one on a replacement Worker, sends the same requestid.
func (RecordPaymentOperation) IdempotencyKey(callID sdkgo.CallID, _ RecordPaymentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke records the payment with POST /payment under the Step's requestid and checks that the
// returned payment applies the amount to the invoice. The send runs under a single-dispatch fence
// capped by call.MaxSends: an unknown outcome is resent with the same requestid while the cap allows
// another send and selects uncertain at the cap, and an accepted but unusable answer is read back
// by its payment ID, which every later attempt reads back instead of resending.
func (operation RecordPaymentOperation) Invoke(call sdkgo.Call, input RecordPaymentInput) sdkgo.MutationAttempt[Payment] {
	client := operation.client
	attemptBudget := client.newAttemptBudget(call.Context, RecordPaymentDefinition.StepDefaults.ExecuteMethodTimeout)
	requested := Payment{CustomerID: strings.TrimSpace(input.CustomerID), TotalAmount: input.Amount}
	payload, err := buildRecordPaymentRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(RecordPaymentBranchDefect, requested,
			quickbooksFailurePointer(sdkgo.FailureValidation, recordPaymentOperationID, err.Error()), sdkgo.Receipt{})
	}
	write, attempt, isDecided := newFencedWrite(client, call, recordPaymentOperationID, RecordPaymentDefinition.StepDefaults.ExecuteMethodTimeout, requested,
		mutationBranches{providerRejected: RecordPaymentBranchProviderRejected, defect: RecordPaymentBranchDefect},
		acceptedWriteReadBack[Payment]{entityPath: "payment", happy: RecordPaymentBranchRecorded,
			confirm: func(body []byte, paymentID string) (Payment, bool) {
				return confirmRecordedPayment(body, paymentID, payload, input.Amount)
			}})
	if isDecided {
		return attempt
	}
	result, notSent := write.dispatch(quickbooksRequest{method: http.MethodPost, path: "/payment", payload: payload, isMutation: true, attemptBudget: attemptBudget})
	if notSent != nil {
		return *notSent
	}
	receipt := client.receipt(call, result, "")
	if attempt, isTerminal := write.attemptForExchange(result, receipt); isTerminal {
		return attempt
	}
	wire, err := decodeEntity[paymentWire](result.response.body, "Payment")
	var payment Payment
	if err == nil {
		payment, err = decodePayment(wire)
	}
	if err == nil && (payment.CustomerID != payload.CustomerRef.Value || !appliesToInvoice(payment, payload.Line[0].LinkedTxn[0].TxnID)) {
		err = errors.New("the returned payment does not apply to the invoice")
	}
	if err != nil {
		return write.unusableAccepted(acceptedEntityID(result.response.body, "Payment"), receipt, err)
	}
	receipt.ProviderObjectID = payment.PaymentID
	return sdkgo.NewMutationBranch(RecordPaymentBranchRecorded, payment, nil, receipt)
}

// confirmRecordedPayment decodes a read payment and reports whether it is the one this Step recorded.
func confirmRecordedPayment(body []byte, paymentID string, payload recordPaymentRequestWire, amount Decimal) (Payment, bool) {
	wire, err := decodeEntity[paymentWire](body, "Payment")
	if err != nil {
		return Payment{}, false
	}
	payment, err := decodePayment(wire)
	return payment, err == nil && payment.PaymentID == paymentID && payment.CustomerID == payload.CustomerRef.Value &&
		areDecimalsNumericallyEqual(payment.TotalAmount, amount) && appliesToInvoice(payment, payload.Line[0].LinkedTxn[0].TxnID)
}

// buildRecordPaymentRequest applies the whole amount to the one invoice, deterministically.
func buildRecordPaymentRequest(input RecordPaymentInput) (recordPaymentRequestWire, error) {
	customerID, invoiceID := strings.TrimSpace(input.CustomerID), strings.TrimSpace(input.InvoiceID)
	if err := validateEntityID("customerId", customerID); err != nil {
		return recordPaymentRequestWire{}, err
	}
	if err := validateEntityID("invoiceId", invoiceID); err != nil {
		return recordPaymentRequestWire{}, err
	}
	if err := validateInputDecimal("amount", input.Amount, paymentAmountRules); err != nil {
		return recordPaymentRequestWire{}, err
	}
	payload := recordPaymentRequestWire{
		CustomerRef: referenceWire{Value: customerID}, TotalAmt: jsonNumberLiteral(input.Amount), TxnDate: strings.TrimSpace(input.TxnDate),
		Line:          []paymentLineInputWire{{Amount: jsonNumberLiteral(input.Amount), LinkedTxn: []linkedTxnWire{{TxnID: invoiceID, TxnType: "Invoice"}}}},
		PaymentRefNum: strings.TrimSpace(input.PaymentReferenceNumber), PrivateNote: input.PrivateNote,
	}
	if err := validateOptionalCalendarDate("txnDate", payload.TxnDate); err != nil {
		return recordPaymentRequestWire{}, err
	}
	if err := validateText("paymentReferenceNumber", payload.PaymentRefNum, maximumPaymentReferenceCharacters, false); err != nil {
		return recordPaymentRequestWire{}, err
	}
	if err := validateText("privateNote", payload.PrivateNote, maximumPrivateNoteCharacters, true); err != nil {
		return recordPaymentRequestWire{}, err
	}
	for _, reference := range []struct {
		name   string
		value  string
		target **referenceWire
	}{
		{"depositAccountId", strings.TrimSpace(input.DepositAccountID), &payload.DepositToAccountRef},
		{"paymentMethodId", strings.TrimSpace(input.PaymentMethodID), &payload.PaymentMethodRef},
	} {
		if reference.value == "" {
			continue
		}
		if err := validateEntityID(reference.name, reference.value); err != nil {
			return recordPaymentRequestWire{}, err
		}
		*reference.target = &referenceWire{Value: reference.value}
	}
	if currencyCode := strings.TrimSpace(input.CurrencyCode); currencyCode != "" {
		if err := validateCurrencyCode("currencyCode", currencyCode); err != nil {
			return recordPaymentRequestWire{}, err
		}
		payload.CurrencyRef = &referenceWire{Value: currencyCode}
	}
	return payload, nil
}

func appliesToInvoice(payment Payment, invoiceID string) bool {
	for _, application := range payment.AppliedInvoices {
		if application.InvoiceID == invoiceID {
			return true
		}
	}
	return false
}
