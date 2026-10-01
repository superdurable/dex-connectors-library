// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	getInvoiceOperationID = "getInvoice"
	// MaxInvoiceNumberCharacters is Xero's InvoiceNumber limit.
	MaxInvoiceNumberCharacters = 255
	// unitAmountDecimalPlaces asks Xero for four-decimal unit amounts on every invoice request.
	unitAmountDecimalPlaces = "4"
)

// GetInvoiceInput identifies one invoice by exactly one of its identifiers.
type GetInvoiceInput struct {
	// InvoiceID is Xero's invoice UUID.
	InvoiceID string `json:"invoiceId,omitempty"`
	// InvoiceNumber is the document number, such as INV-0042, used when InvoiceID is blank.
	InvoiceNumber string `json:"invoiceNumber,omitempty"`
}

// GetInvoiceOperation implements the getInvoice Query.
type GetInvoiceOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetInvoiceOperation) Definition() sdkgo.QueryDefinition { return GetInvoiceDefinition }

// Invoke reads GET /Invoices/{InvoiceID or InvoiceNumber} with four-decimal unit amounts.
func (operation GetInvoiceOperation) Invoke(call sdkgo.Call, input GetInvoiceInput) sdkgo.QueryAttempt[Invoice] {
	client := operation.client
	identifier, err := resolveInvoiceIdentifier(input)
	if err != nil {
		return sdkgo.NewQueryBranch(GetInvoiceBranchDefect, Invoice{}, xeroFailurePointer(sdkgo.FailureValidation, getInvoiceOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, getInvoiceOperationID, xeroRequest{
		method: http.MethodGet, path: "/Invoices/" + url.PathEscape(identifier), query: url.Values{"unitdp": {unitAmountDecimalPlaces}},
	})
	receipt := client.receipt(call, result, "")
	if attempt, isTerminal := queryAttemptForExchange(result, Invoice{}, receipt, queryBranches{
		notFound: GetInvoiceBranchNotFound, providerRejected: GetInvoiceBranchProviderRejected,
		dailyLimitReached: GetInvoiceBranchDailyLimitReached, invalidResponse: GetInvoiceBranchInvalidResponse, defect: GetInvoiceBranchDefect,
	}); isTerminal {
		return attempt
	}
	wire, err := decodeSingleResource[invoiceWire](result.response.body, "Invoices")
	if err == nil && !strings.EqualFold(wire.InvoiceID, identifier) && wire.InvoiceNumber != identifier {
		err = errors.New("response describes another invoice")
	}
	var invoice Invoice
	if err == nil {
		invoice, err = decodeInvoice(wire)
	}
	if err != nil {
		return sdkgo.NewQueryBranch(GetInvoiceBranchInvalidResponse, Invoice{},
			xeroFailurePointer(sdkgo.FailureProtocol, getInvoiceOperationID, "Xero returned an invalid invoice: "+err.Error()), receipt)
	}
	receipt.ProviderObjectID = invoice.InvoiceID
	return sdkgo.NewQueryBranch(GetInvoiceBranchFound, invoice, nil, receipt)
}

func resolveInvoiceIdentifier(input GetInvoiceInput) (string, error) {
	invoiceID, invoiceNumber := strings.TrimSpace(input.InvoiceID), strings.TrimSpace(input.InvoiceNumber)
	switch {
	case invoiceID != "" && invoiceNumber != "":
		return "", errors.New("set either invoiceId or invoiceNumber, not both")
	case invoiceID != "":
		if !uuidPattern.MatchString(invoiceID) {
			return "", errors.New("invoiceId must be a Xero invoice UUID such as 243216c5-369e-4056-ac67-05388f86dc81")
		}
		return strings.ToLower(invoiceID), nil
	case invoiceNumber != "":
		return invoiceNumber, validateInvoiceNumber("invoiceNumber", invoiceNumber)
	default:
		return "", errors.New("invoiceId or invoiceNumber is required")
	}
}

// validateInvoiceNumber accepts the printable ASCII Xero documents for invoice numbers.
func validateInvoiceNumber(fieldName string, value string) error {
	if len(value) > MaxInvoiceNumberCharacters {
		return errors.New(fieldName + " can be at most 255 characters")
	}
	for index := 0; index < len(value); index++ {
		if value[index] < ' ' || value[index] > '~' || value[index] == '"' || value[index] == '\\' {
			return errors.New(fieldName + " must be printable ASCII without double quotes or backslashes")
		}
	}
	return nil
}
