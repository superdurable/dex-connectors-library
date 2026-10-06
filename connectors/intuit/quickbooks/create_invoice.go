// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	createInvoiceOperationID = "createInvoice"
	// MaxInvoiceLines bounds one created invoice so the request and Result stay small.
	MaxInvoiceLines = 100
	// maximumLineDescriptionCharacters is QuickBooks's line Description limit.
	maximumLineDescriptionCharacters = 4000
	// maximumCustomerMemoCharacters is QuickBooks's CustomerMemo limit.
	maximumCustomerMemoCharacters = 1000
	// maximumPrivateNoteCharacters is QuickBooks's PrivateNote limit.
	maximumPrivateNoteCharacters = 4000
)

var (
	// taxCodePattern matches a QuickBooks tax code Id, such as TAX, NON, or 3.
	taxCodePattern = regexp.MustCompile(`^[A-Za-z0-9]{1,20}$`)

	quantityRules  = decimalRules{maximumFractionDigits: 5, maximumIntegerDigits: 12, canBeZero: true}
	unitPriceRules = decimalRules{maximumFractionDigits: 5, maximumIntegerDigits: 12, canBeNegative: true, canBeZero: true}
	// lineAmountRules keeps QuickBooks's documented 10.5 Amount format to whole cents.
	lineAmountRules = decimalRules{maximumFractionDigits: 2, maximumIntegerDigits: 10, canBeNegative: true, canBeZero: true}
)

// CreateInvoiceInput describes one new invoice. Amounts are exact Decimals in the customer's currency.
type CreateInvoiceInput struct {
	// CustomerID is the invoiced customer's Id, such as from findCustomer or createCustomer.
	CustomerID string `json:"customerId"`
	// Lines are 1 to 100 item lines.
	Lines []InvoiceLineInput `json:"lines"`
	// TxnDate is the invoice date, YYYY-MM-DD; blank uses today in the company's time zone.
	TxnDate string `json:"txnDate,omitempty"`
	// DueDate is the payment due date, YYYY-MM-DD; blank applies the customer's or company's terms.
	DueDate string `json:"dueDate,omitempty"`
	// DocNumber is the invoice number, at most 21 characters without a backslash, such as an order ID,
	// so listInvoices can search for it; blank lets QuickBooks assign the next number when the company
	// numbers invoices automatically.
	DocNumber string `json:"docNumber,omitempty"`
	// BillEmail is the address sendInvoice uses by default; blank uses none.
	BillEmail string `json:"billEmail,omitempty"`
	// CurrencyCode is the invoice's ISO 4217 currency, which must be the customer's; blank uses
	// the customer's currency.
	CurrencyCode string `json:"currencyCode,omitempty"`
	// CustomerMemo is the message shown to the customer on the invoice, at most 1000 characters.
	CustomerMemo string `json:"customerMemo,omitempty"`
	// PrivateNote is an internal memo, at most 4000 characters, such as the application's order ID.
	PrivateNote string `json:"privateNote,omitempty"`
}

// InvoiceLineInput is one item line of a new invoice.
type InvoiceLineInput struct {
	// ItemID is the product or service item's Id, which supplies the income account.
	ItemID string `json:"itemId"`
	// Description is the line text, at most 4000 characters; blank uses the item's description.
	Description string `json:"description,omitempty"`
	// Quantity is the item quantity, up to five decimal places.
	Quantity Decimal `json:"quantity,omitempty"`
	// UnitPrice is the unit price, up to five decimal places.
	UnitPrice Decimal `json:"unitPrice,omitempty"`
	// Amount is the line amount with at most two decimal places. Blank computes Quantity times
	// UnitPrice exactly, rounded half away from zero to two places; both are then required.
	Amount Decimal `json:"amount,omitempty"`
	// TaxCodeID is the line's tax code, such as TAX or NON in a US company; blank uses the item's.
	TaxCodeID string `json:"taxCodeId,omitempty"`
	// ServiceDate is the date of service, YYYY-MM-DD; blank leaves it unset.
	ServiceDate string `json:"serviceDate,omitempty"`
}

// CreateInvoiceOutput is the created invoice. On every other branch Invoice is empty and
// DocNumber echoes the request, so an application can reconcile with listInvoices.
type CreateInvoiceOutput struct {
	// Invoice is the invoice QuickBooks created or, for a replayed attempt, created earlier for this Step.
	Invoice Invoice `json:"invoice"`
	// DocNumber echoes the requested document number.
	DocNumber string `json:"docNumber,omitempty"`
}

// CreateInvoiceOperation implements the createInvoice Mutation.
type CreateInvoiceOperation struct{ client *Client }

type createInvoiceRequestWire struct {
	CustomerRef  referenceWire     `json:"CustomerRef"`
	Line         []createLineWire  `json:"Line"`
	TxnDate      string            `json:"TxnDate,omitempty"`
	DueDate      string            `json:"DueDate,omitempty"`
	DocNumber    string            `json:"DocNumber,omitempty"`
	BillEmail    *emailAddressWire `json:"BillEmail,omitempty"`
	CurrencyRef  *referenceWire    `json:"CurrencyRef,omitempty"`
	CustomerMemo *customerMemoWire `json:"CustomerMemo,omitempty"`
	PrivateNote  string            `json:"PrivateNote,omitempty"`
}

type customerMemoWire struct {
	Value string `json:"value"`
}

type createLineWire struct {
	DetailType          string              `json:"DetailType"`
	Amount              json.Number         `json:"Amount"`
	Description         string              `json:"Description,omitempty"`
	SalesItemLineDetail salesItemDetailWire `json:"SalesItemLineDetail"`
}

type salesItemDetailWire struct {
	ItemRef     referenceWire  `json:"ItemRef"`
	Qty         json.Number    `json:"Qty,omitempty"`
	UnitPrice   json.Number    `json:"UnitPrice,omitempty"`
	TaxCodeRef  *referenceWire `json:"TaxCodeRef,omitempty"`
	ServiceDate string         `json:"ServiceDate,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (CreateInvoiceOperation) Definition() sdkgo.MutationDefinition { return CreateInvoiceDefinition }

// IdempotencyKey is the stable Call ID, sent as QuickBooks's requestid. Every attempt of one Step
// execution, including one on a replacement Worker, sends the same requestid.
func (CreateInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateInvoiceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates the invoice with POST /invoice under the Step's requestid.
func (operation CreateInvoiceOperation) Invoke(call sdkgo.Call, input CreateInvoiceInput) sdkgo.MutationAttempt[CreateInvoiceOutput] {
	client := operation.client
	requested := CreateInvoiceOutput{DocNumber: strings.TrimSpace(input.DocNumber)}
	payload, err := buildCreateInvoiceRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateInvoiceBranchDefect, requested,
			quickbooksFailurePointer(sdkgo.FailureValidation, createInvoiceOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, createInvoiceOperationID, quickbooksRequest{method: http.MethodPost, path: "/invoice", payload: payload, isMutation: true})
	receipt := client.receipt(call, result, "")
	if attempt, isTerminal := mutationAttemptForExchange(result, requested, receipt, mutationBranches{
		providerRejected: CreateInvoiceBranchProviderRejected, defect: CreateInvoiceBranchDefect,
	}); isTerminal {
		return attempt
	}
	wire, err := decodeEntity[invoiceWire](result.response.body, "Invoice")
	var invoice Invoice
	if err == nil {
		invoice, err = decodeInvoice(wire)
	}
	if err == nil && invoice.CustomerID != payload.CustomerRef.Value {
		err = errors.New("invoice belongs to another customer")
	}
	if err != nil {
		return sdkgo.NewMutationUncertain(requested, quickbooksFailure(sdkgo.FailureProtocol, createInvoiceOperationID,
			"QuickBooks accepted the invoice but returned an unusable invoice: "+err.Error()), receipt)
	}
	receipt.ProviderObjectID = invoice.InvoiceID
	return sdkgo.NewMutationBranch(CreateInvoiceBranchCreated, CreateInvoiceOutput{Invoice: invoice, DocNumber: requested.DocNumber}, nil, receipt)
}

// buildCreateInvoiceRequest is deterministic for one input, so every attempt sends the same body.
func buildCreateInvoiceRequest(input CreateInvoiceInput) (createInvoiceRequestWire, error) {
	payload := createInvoiceRequestWire{
		CustomerRef: referenceWire{Value: strings.TrimSpace(input.CustomerID)}, TxnDate: strings.TrimSpace(input.TxnDate),
		DueDate: strings.TrimSpace(input.DueDate), DocNumber: strings.TrimSpace(input.DocNumber), PrivateNote: input.PrivateNote,
	}
	if err := validateEntityID("customerId", payload.CustomerRef.Value); err != nil {
		return createInvoiceRequestWire{}, err
	}
	if err := validateOptionalCalendarDate("txnDate", payload.TxnDate); err != nil {
		return createInvoiceRequestWire{}, err
	}
	if err := validateOptionalCalendarDate("dueDate", payload.DueDate); err != nil {
		return createInvoiceRequestWire{}, err
	}
	if payload.DueDate != "" && payload.TxnDate != "" && payload.DueDate < payload.TxnDate {
		return createInvoiceRequestWire{}, errors.New("dueDate cannot be before txnDate")
	}
	// listInvoices must be able to search for the docNumber, so the query literal rules apply.
	if err := validateQueryLiteral("docNumber", payload.DocNumber, MaxDocNumberCharacters); err != nil {
		return createInvoiceRequestWire{}, err
	}
	if billEmail := strings.TrimSpace(input.BillEmail); billEmail != "" {
		if err := validateEmailAddress("billEmail", billEmail); err != nil {
			return createInvoiceRequestWire{}, err
		}
		payload.BillEmail = &emailAddressWire{Address: billEmail}
	}
	if currencyCode := strings.TrimSpace(input.CurrencyCode); currencyCode != "" {
		if err := validateCurrencyCode("currencyCode", currencyCode); err != nil {
			return createInvoiceRequestWire{}, err
		}
		payload.CurrencyRef = &referenceWire{Value: currencyCode}
	}
	if input.CustomerMemo != "" {
		if err := validateText("customerMemo", input.CustomerMemo, maximumCustomerMemoCharacters, true); err != nil {
			return createInvoiceRequestWire{}, err
		}
		payload.CustomerMemo = &customerMemoWire{Value: input.CustomerMemo}
	}
	if err := validateText("privateNote", input.PrivateNote, maximumPrivateNoteCharacters, true); err != nil {
		return createInvoiceRequestWire{}, err
	}
	if len(input.Lines) == 0 || len(input.Lines) > MaxInvoiceLines {
		return createInvoiceRequestWire{}, fmt.Errorf("lines must hold 1 to %d lines", MaxInvoiceLines)
	}
	for index, line := range input.Lines {
		wire, err := buildCreateLine(fmt.Sprintf("lines[%d]", index), line)
		if err != nil {
			return createInvoiceRequestWire{}, err
		}
		payload.Line = append(payload.Line, wire)
	}
	return payload, nil
}

func buildCreateLine(fieldName string, line InvoiceLineInput) (createLineWire, error) {
	wire := createLineWire{
		DetailType: string(LineDetailTypeSalesItem), Description: line.Description,
		SalesItemLineDetail: salesItemDetailWire{ItemRef: referenceWire{Value: strings.TrimSpace(line.ItemID)}, ServiceDate: strings.TrimSpace(line.ServiceDate)},
	}
	if err := validateEntityID(fieldName+".itemId", wire.SalesItemLineDetail.ItemRef.Value); err != nil {
		return createLineWire{}, err
	}
	if err := validateText(fieldName+".description", line.Description, maximumLineDescriptionCharacters, true); err != nil {
		return createLineWire{}, err
	}
	if err := validateOptionalCalendarDate(fieldName+".serviceDate", wire.SalesItemLineDetail.ServiceDate); err != nil {
		return createLineWire{}, err
	}
	if taxCode := strings.TrimSpace(line.TaxCodeID); taxCode != "" {
		if !taxCodePattern.MatchString(taxCode) {
			return createLineWire{}, fmt.Errorf("%s.taxCodeId must be a QuickBooks tax code such as TAX, NON, or 3", fieldName)
		}
		wire.SalesItemLineDetail.TaxCodeRef = &referenceWire{Value: taxCode}
	}
	for _, amount := range []struct {
		name   string
		value  Decimal
		rules  decimalRules
		target *json.Number
	}{
		{name: "quantity", value: line.Quantity, rules: quantityRules, target: &wire.SalesItemLineDetail.Qty},
		{name: "unitPrice", value: line.UnitPrice, rules: unitPriceRules, target: &wire.SalesItemLineDetail.UnitPrice},
		{name: "amount", value: line.Amount, rules: lineAmountRules, target: &wire.Amount},
	} {
		if amount.value == "" {
			continue
		}
		if err := validateInputDecimal(fieldName+"."+amount.name, amount.value, amount.rules); err != nil {
			return createLineWire{}, err
		}
		*amount.target = jsonNumberLiteral(amount.value)
	}
	if line.Amount == "" {
		if line.Quantity == "" || line.UnitPrice == "" {
			return createLineWire{}, fmt.Errorf("%s needs an amount, or both quantity and unitPrice", fieldName)
		}
		computed, err := multiplyRoundedToCents(line.Quantity, line.UnitPrice)
		if err != nil {
			return createLineWire{}, fmt.Errorf("%s amount cannot be computed", fieldName)
		}
		// The product always has two places, so only its integer digits can break the 10.5 Amount format.
		if err := validateInputDecimal(fieldName+".amount", computed, lineAmountRules); err != nil {
			return createLineWire{}, fmt.Errorf("%s quantity times unitPrice can have at most %d digits before the decimal point",
				fieldName, lineAmountRules.maximumIntegerDigits)
		}
		wire.Amount = jsonNumberLiteral(computed)
	}
	return wire, nil
}
