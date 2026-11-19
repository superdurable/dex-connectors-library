// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const sendInvoiceOperationID = "sendInvoice"

// SendInvoiceInput identifies the invoice to email and, optionally, its recipient.
type SendInvoiceInput struct {
	// InvoiceID is the invoice's QuickBooks Id, such as 130.
	InvoiceID string `json:"invoiceId"`
	// SendTo is the recipient address; QuickBooks also stores it as the invoice's BillEmail.
	// Blank sends to the invoice's BillEmail, and QuickBooks rejects an invoice without one.
	SendTo string `json:"sendTo,omitempty"`
}

// SendInvoiceOperation implements the sendInvoice Mutation.
type SendInvoiceOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (SendInvoiceOperation) Definition() sdkgo.MutationDefinition { return SendInvoiceDefinition }

// IdempotencyKey is the stable Call ID, sent as QuickBooks's requestid. Every attempt of one Step
// execution, including one on a replacement Worker, sends the same requestid.
func (SendInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ SendInvoiceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /invoice/{invoiceId}/send with an empty application/octet-stream body under
// the Step's requestid and returns the invoice QuickBooks updated, with EmailStatus EmailSent. The
// send runs under a single-dispatch fence capped by call.MaxSends: an unknown outcome is resent with
// the same requestid while the cap allows another send and selects uncertain at the cap, and an
// accepted but unusable answer reads the invoice back, as every later attempt then does.
func (operation SendInvoiceOperation) Invoke(call sdkgo.Call, input SendInvoiceInput) sdkgo.MutationAttempt[Invoice] {
	client := operation.client
	attemptBudget := client.newAttemptBudget(call.Context, SendInvoiceDefinition.StepDefaults.ExecuteMethodTimeout)
	invoiceID, sendTo := strings.TrimSpace(input.InvoiceID), strings.TrimSpace(input.SendTo)
	requested := Invoice{InvoiceID: invoiceID}
	if err := validateEntityID("invoiceId", invoiceID); err != nil {
		return sdkgo.NewMutationBranch(SendInvoiceBranchDefect, requested,
			quickbooksFailurePointer(sdkgo.FailureValidation, sendInvoiceOperationID, err.Error()), sdkgo.Receipt{})
	}
	query := url.Values{}
	if sendTo != "" {
		if err := validateEmailAddress("sendTo", sendTo); err != nil {
			return sdkgo.NewMutationBranch(SendInvoiceBranchDefect, requested,
				quickbooksFailurePointer(sdkgo.FailureValidation, sendInvoiceOperationID, err.Error()), sdkgo.Receipt{})
		}
		query.Set("sendTo", sendTo)
	}
	write, attempt, isDecided := newFencedWrite(client, call, sendInvoiceOperationID, SendInvoiceDefinition.StepDefaults.ExecuteMethodTimeout, requested,
		mutationBranches{providerRejected: SendInvoiceBranchProviderRejected, defect: SendInvoiceBranchDefect},
		acceptedWriteReadBack[Invoice]{entityPath: "invoice", happy: SendInvoiceBranchSent,
			confirm: func(body []byte, readInvoiceID string) (Invoice, bool) {
				return confirmSentInvoice(body, readInvoiceID, sendTo)
			}})
	if isDecided {
		return attempt
	}
	result, notSent := write.dispatch(quickbooksRequest{
		method: http.MethodPost, path: "/invoice/" + invoiceID + "/send", query: query, isMutation: true, attemptBudget: attemptBudget,
	})
	if notSent != nil {
		return *notSent
	}
	receipt := client.receipt(call, result, invoiceID)
	if attempt, isTerminal := write.attemptForExchange(result, receipt); isTerminal {
		return attempt
	}
	wire, err := decodeEntity[invoiceWire](result.response.body, "Invoice")
	var invoice Invoice
	if err == nil {
		invoice, err = decodeInvoice(wire)
	}
	if err == nil && invoice.InvoiceID != invoiceID {
		err = errors.New("QuickBooks returned another invoice")
	}
	if err == nil && invoice.EmailStatus != EmailStatusEmailSent {
		err = errors.New("the returned invoice is not marked EmailSent")
	}
	if err == nil && sendTo != "" && !strings.EqualFold(invoice.BillEmail, sendTo) {
		err = errors.New("the returned invoice has another billing address")
	}
	if err != nil {
		return write.unusableAccepted(invoiceID, receipt, err)
	}
	return sdkgo.NewMutationBranch(SendInvoiceBranchSent, invoice, nil, receipt)
}

// confirmSentInvoice decodes a read invoice and reports whether it shows the delivery this Step requested.
func confirmSentInvoice(body []byte, invoiceID string, sendTo string) (Invoice, bool) {
	wire, err := decodeEntity[invoiceWire](body, "Invoice")
	if err != nil {
		return Invoice{}, false
	}
	invoice, err := decodeInvoice(wire)
	return invoice, err == nil && invoice.InvoiceID == invoiceID && invoice.EmailStatus == EmailStatusEmailSent &&
		(sendTo == "" || strings.EqualFold(invoice.BillEmail, sendTo))
}
