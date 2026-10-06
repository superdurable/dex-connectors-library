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
// the Step's requestid and returns the invoice QuickBooks updated, with EmailStatus EmailSent.
func (operation SendInvoiceOperation) Invoke(call sdkgo.Call, input SendInvoiceInput) sdkgo.MutationAttempt[Invoice] {
	client := operation.client
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
	result := client.exchange(call, sendInvoiceOperationID, quickbooksRequest{
		method: http.MethodPost, path: "/invoice/" + invoiceID + "/send", query: query, isMutation: true,
	})
	receipt := client.receipt(call, result, invoiceID)
	if attempt, isTerminal := mutationAttemptForExchange(result, requested, receipt, mutationBranches{
		providerRejected: SendInvoiceBranchProviderRejected, defect: SendInvoiceBranchDefect,
	}); isTerminal {
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
	if err != nil {
		return sdkgo.NewMutationUncertain(requested, quickbooksFailure(sdkgo.FailureProtocol, sendInvoiceOperationID,
			"QuickBooks accepted the delivery but returned an unusable invoice: "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(SendInvoiceBranchSent, invoice, nil, receipt)
}
