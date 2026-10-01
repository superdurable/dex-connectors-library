// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	createInvoiceOperationID = "createInvoice"
	// MaxInvoiceLineItems bounds one created invoice; Xero recommends batches of at most 50 elements.
	MaxInvoiceLineItems = 100
	// MaxLineDescriptionCharacters is Xero's line item Description limit.
	MaxLineDescriptionCharacters = 4000
)

var (
	currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)
	// accountingCodePattern bounds account, item, and tax type codes to printable tokens.
	accountingCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._/-]{0,49}$`)

	quantityRules     = decimalRules{maximumFractionDigits: 4, maximumIntegerDigits: 12, canBeNegative: true, canBeZero: true}
	unitAmountRules   = decimalRules{maximumFractionDigits: 4, maximumIntegerDigits: 12, canBeNegative: true, canBeZero: true}
	discountRateRules = decimalRules{maximumFractionDigits: 4, maximumIntegerDigits: 3, canBeZero: true}
)

// CreateInvoiceInput describes one new sales invoice or bill. Amounts are exact Decimals in the
// invoice's currency and are sent to Xero as the same number literals.
type CreateInvoiceInput struct {
	// Type is ACCREC for a sales invoice or ACCPAY for a bill.
	Type InvoiceType `json:"type"`
	// ContactID is the Xero UUID of the customer or supplier, such as from findContactByEmail.
	// Xero is sent only this ID, so the contact record is never changed.
	ContactID string `json:"contactId"`
	// LineItems are 1 to 100 lines, each with a description.
	LineItems []InvoiceLineItemInput `json:"lineItems"`
	// Date is the issue date, YYYY-MM-DD; blank uses today in the organisation's time zone.
	Date string `json:"date,omitempty"`
	// DueDate is the due date, YYYY-MM-DD; Xero requires one to approve a sales invoice.
	DueDate string `json:"dueDate,omitempty"`
	// LineAmountTypes is Exclusive, Inclusive, or NoTax; blank means Exclusive.
	LineAmountTypes LineAmountTypes `json:"lineAmountTypes,omitempty"`
	// CurrencyCode is an ISO 4217 code enabled in the organisation, such as USD; blank uses the
	// organisation's base currency.
	CurrencyCode string `json:"currencyCode,omitempty"`
	// InvoiceNumber is the document number. Blank lets Xero number an ACCREC invoice from the
	// organisation's invoice settings; an ACCREC number must be unique.
	InvoiceNumber string `json:"invoiceNumber,omitempty"`
	// Reference is an additional reference, such as the application's order ID; listInvoices can
	// find the invoice by it.
	Reference string `json:"reference,omitempty"`
	// Status is DRAFT, SUBMITTED, or AUTHORISED; blank means DRAFT. Only an AUTHORISED invoice
	// can take a payment.
	Status InvoiceStatus `json:"status,omitempty"`
	// BrandingThemeID is the Xero UUID of a branding theme; blank uses the organisation's default.
	BrandingThemeID string `json:"brandingThemeId,omitempty"`
}

// InvoiceLineItemInput is one line of a new invoice.
type InvoiceLineItemInput struct {
	// Description is 1 to 4000 characters.
	Description string `json:"description"`
	// Quantity is the line quantity, up to four decimal places; blank leaves it to Xero or the item.
	Quantity Decimal `json:"quantity,omitempty"`
	// UnitAmount is the unit price, up to four decimal places, which Xero keeps because the
	// connector sends unitdp=4; blank leaves it to Xero or the item.
	UnitAmount Decimal `json:"unitAmount,omitempty"`
	// AccountCode is the chart-of-accounts code, such as 200; Xero requires one to approve.
	AccountCode string `json:"accountCode,omitempty"`
	// ItemCode is an inventory item code, which can supply the description and price.
	ItemCode string `json:"itemCode,omitempty"`
	// TaxType overrides the account's default Xero tax type, such as OUTPUT or NONE.
	TaxType string `json:"taxType,omitempty"`
	// DiscountRate is a percentage from 0 to 100 on an ACCREC line; ACCPAY lines take none.
	DiscountRate Decimal `json:"discountRate,omitempty"`
}

// CreateInvoiceOutput is the created invoice. On every other branch Invoice is empty and
// Reference echoes the request, so an application can reconcile with listInvoices.
type CreateInvoiceOutput struct {
	// Invoice is the invoice Xero created or, for a replayed attempt, created earlier for this Step.
	Invoice Invoice `json:"invoice"`
	// Reference echoes the requested reference.
	Reference string `json:"reference,omitempty"`
	// WarningCount is the number of validation warnings Xero attached to the accepted invoice,
	// such as a questionable currency rate; their text is not exposed.
	WarningCount int `json:"warningCount,omitempty"`
}

// CreateInvoiceOperation implements the createInvoice Mutation.
type CreateInvoiceOperation struct{ client *Client }

type createInvoiceRequestWire struct {
	Type            string               `json:"Type"`
	Contact         contactReferenceWire `json:"Contact"`
	Date            string               `json:"Date,omitempty"`
	DueDate         string               `json:"DueDate,omitempty"`
	LineAmountTypes string               `json:"LineAmountTypes,omitempty"`
	LineItems       []createLineItemWire `json:"LineItems"`
	CurrencyCode    string               `json:"CurrencyCode,omitempty"`
	InvoiceNumber   string               `json:"InvoiceNumber,omitempty"`
	Reference       string               `json:"Reference,omitempty"`
	Status          string               `json:"Status,omitempty"`
	BrandingThemeID string               `json:"BrandingThemeID,omitempty"`
}

type contactReferenceWire struct {
	ContactID string `json:"ContactID"`
}

type createLineItemWire struct {
	Description  string      `json:"Description"`
	Quantity     json.Number `json:"Quantity,omitempty"`
	UnitAmount   json.Number `json:"UnitAmount,omitempty"`
	AccountCode  string      `json:"AccountCode,omitempty"`
	ItemCode     string      `json:"ItemCode,omitempty"`
	TaxType      string      `json:"TaxType,omitempty"`
	DiscountRate json.Number `json:"DiscountRate,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (CreateInvoiceOperation) Definition() sdkgo.MutationDefinition { return CreateInvoiceDefinition }

// IdempotencyKey is the stable Call ID, sent as Xero's Idempotency-Key header. Every attempt of one
// Step execution, including one on a replacement Worker, sends the same key.
func (CreateInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateInvoiceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates the invoice with PUT /Invoices, which only creates. A rate limit, a lost
// response, or a 502, 503, or 504 is retried with the identical body under the same key, and
// Xero answers a repeated key with its cached response instead of creating a second invoice.
func (operation CreateInvoiceOperation) Invoke(call sdkgo.Call, input CreateInvoiceInput) sdkgo.MutationAttempt[CreateInvoiceOutput] {
	client := operation.client
	requested := CreateInvoiceOutput{Reference: strings.TrimSpace(input.Reference)}
	payload, err := buildCreateInvoiceRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateInvoiceBranchDefect, requested, xeroFailurePointer(sdkgo.FailureValidation, createInvoiceOperationID, err.Error()), sdkgo.Receipt{})
	}
	result := client.exchange(call, createInvoiceOperationID, xeroRequest{
		method: http.MethodPut, path: "/Invoices", query: url.Values{"unitdp": {unitAmountDecimalPlaces}}, payload: payload, isMutation: true,
	})
	receipt := client.receipt(call, result, "")
	if attempt, isTerminal := mutationAttemptForExchange(result, requested, receipt, mutationBranches{
		providerRejected: CreateInvoiceBranchProviderRejected, dailyLimitReached: CreateInvoiceBranchDailyLimitReached, defect: CreateInvoiceBranchDefect,
	}); isTerminal {
		return attempt
	}
	wire, err := decodeSingleResource[invoiceWire](result.response.body, "Invoices")
	if err == nil && rejectsInvoiceElement(wire) {
		return sdkgo.NewMutationBranch(CreateInvoiceBranchProviderRejected, requested, xeroFailurePointer(sdkgo.FailureValidation, createInvoiceOperationID,
			withErrorSummary("Xero rejected the invoice", xeroErrorSummary{validationErrorCount: len(wire.ValidationErrors)})), receipt)
	}
	var invoice Invoice
	if err == nil {
		invoice, err = decodeInvoice(wire)
	}
	if err != nil {
		return sdkgo.NewMutationUncertain(requested, xeroFailure(sdkgo.FailureProtocol, createInvoiceOperationID,
			"Xero accepted the invoice but returned an unusable invoice: "+err.Error()), receipt)
	}
	receipt.ProviderObjectID = invoice.InvoiceID
	return sdkgo.NewMutationBranch(CreateInvoiceBranchCreated, CreateInvoiceOutput{
		Invoice: invoice, Reference: requested.Reference, WarningCount: len(wire.Warnings),
	}, nil, receipt)
}

// buildCreateInvoiceRequest is deterministic, because Xero rejects a repeated key whose request differs.
func buildCreateInvoiceRequest(input CreateInvoiceInput) (createInvoiceRequestWire, error) {
	if err := validateInvoiceType(input.Type); err != nil {
		return createInvoiceRequestWire{}, err
	}
	contactID := strings.TrimSpace(input.ContactID)
	if !uuidPattern.MatchString(contactID) {
		return createInvoiceRequestWire{}, errors.New("contactId must be a Xero contact UUID such as eaa28f49-6028-4b6e-bb12-d8f6278073fc")
	}
	payload := createInvoiceRequestWire{
		Type: string(input.Type), Contact: contactReferenceWire{ContactID: strings.ToLower(contactID)},
		Date: strings.TrimSpace(input.Date), DueDate: strings.TrimSpace(input.DueDate),
		CurrencyCode: strings.TrimSpace(input.CurrencyCode), InvoiceNumber: strings.TrimSpace(input.InvoiceNumber),
		Reference: strings.TrimSpace(input.Reference), Status: string(input.Status),
		LineAmountTypes: string(input.LineAmountTypes), BrandingThemeID: strings.ToLower(strings.TrimSpace(input.BrandingThemeID)),
	}
	for _, date := range []struct{ fieldName, value string }{{"date", payload.Date}, {"dueDate", payload.DueDate}} {
		if date.value == "" {
			continue
		}
		if err := validateCalendarDate(date.fieldName, date.value); err != nil {
			return createInvoiceRequestWire{}, err
		}
	}
	switch input.LineAmountTypes {
	case "", LineAmountTypesExclusive, LineAmountTypesInclusive, LineAmountTypesNoTax:
	default:
		return createInvoiceRequestWire{}, errors.New("lineAmountTypes must be Exclusive, Inclusive, or NoTax")
	}
	switch input.Status {
	case "", InvoiceStatusDraft, InvoiceStatusSubmitted, InvoiceStatusAuthorised:
	default:
		return createInvoiceRequestWire{}, errors.New("status of a new invoice must be DRAFT, SUBMITTED, or AUTHORISED")
	}
	if payload.CurrencyCode != "" && !currencyCodePattern.MatchString(payload.CurrencyCode) {
		return createInvoiceRequestWire{}, errors.New("currencyCode must be a three-letter ISO 4217 code such as USD")
	}
	if err := validateInvoiceNumber("invoiceNumber", payload.InvoiceNumber); err != nil {
		return createInvoiceRequestWire{}, err
	}
	if err := validateSingleLineText("reference", payload.Reference, MaxReferenceCharacters); err != nil {
		return createInvoiceRequestWire{}, err
	}
	if payload.BrandingThemeID != "" && !uuidPattern.MatchString(payload.BrandingThemeID) {
		return createInvoiceRequestWire{}, errors.New("brandingThemeId must be a Xero branding theme UUID")
	}
	if len(input.LineItems) == 0 || len(input.LineItems) > MaxInvoiceLineItems {
		return createInvoiceRequestWire{}, fmt.Errorf("lineItems must hold 1 to %d lines", MaxInvoiceLineItems)
	}
	for index, line := range input.LineItems {
		wire, err := buildCreateLineItem(fmt.Sprintf("lineItems[%d]", index), line, input.Type)
		if err != nil {
			return createInvoiceRequestWire{}, err
		}
		payload.LineItems = append(payload.LineItems, wire)
	}
	return payload, nil
}

func buildCreateLineItem(fieldName string, line InvoiceLineItemInput, invoiceType InvoiceType) (createLineItemWire, error) {
	description := strings.TrimSpace(line.Description)
	switch {
	case description == "":
		return createLineItemWire{}, fmt.Errorf("%s.description is required", fieldName)
	case !utf8.ValidString(description) || utf8.RuneCountInString(description) > MaxLineDescriptionCharacters:
		return createLineItemWire{}, fmt.Errorf("%s.description must be valid UTF-8 of at most %d characters", fieldName, MaxLineDescriptionCharacters)
	}
	wire := createLineItemWire{
		Description: description, AccountCode: strings.TrimSpace(line.AccountCode),
		ItemCode: strings.TrimSpace(line.ItemCode), TaxType: strings.TrimSpace(line.TaxType),
	}
	for _, code := range []struct{ name, value string }{{"accountCode", wire.AccountCode}, {"itemCode", wire.ItemCode}, {"taxType", wire.TaxType}} {
		if code.value != "" && !accountingCodePattern.MatchString(code.value) {
			return createLineItemWire{}, fmt.Errorf("%s.%s must be a Xero code of at most 50 letters, digits, spaces, or . _ / -", fieldName, code.name)
		}
	}
	for _, amount := range []struct {
		name   string
		value  Decimal
		rules  decimalRules
		target *json.Number
	}{
		{name: "quantity", value: line.Quantity, rules: quantityRules, target: &wire.Quantity},
		{name: "unitAmount", value: line.UnitAmount, rules: unitAmountRules, target: &wire.UnitAmount},
		{name: "discountRate", value: line.DiscountRate, rules: discountRateRules, target: &wire.DiscountRate},
	} {
		if amount.value == "" {
			continue
		}
		if err := validateInputDecimal(fieldName+"."+amount.name, amount.value, amount.rules); err != nil {
			return createLineItemWire{}, err
		}
		*amount.target = jsonNumberLiteral(amount.value)
	}
	if line.DiscountRate != "" {
		if invoiceType != InvoiceTypeAccountsReceivable {
			return createLineItemWire{}, fmt.Errorf("%s.discountRate is supported only on ACCREC invoices", fieldName)
		}
		if !isAtMostOneHundred(line.DiscountRate) {
			return createLineItemWire{}, fmt.Errorf("%s.discountRate must be a percentage from 0 to 100", fieldName)
		}
	}
	return wire, nil
}

// isAtMostOneHundred compares a validated non-negative decimal with 100 without floating point.
func isAtMostOneHundred(value Decimal) bool {
	integerPart, fractionPart, _ := strings.Cut(string(value), ".")
	return len(integerPart) < 3 || (integerPart == "100" && strings.Trim(fractionPart, "0") == "")
}
