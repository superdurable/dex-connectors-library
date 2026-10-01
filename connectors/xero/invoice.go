// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// InvoiceType is Xero's own invoice type value.
type InvoiceType string

const (
	// InvoiceTypeAccountsReceivable is a sales invoice, ACCREC.
	InvoiceTypeAccountsReceivable InvoiceType = "ACCREC"
	// InvoiceTypeAccountsPayable is a bill, ACCPAY.
	InvoiceTypeAccountsPayable InvoiceType = "ACCPAY"
)

// InvoiceStatus is Xero's own invoice status value.
type InvoiceStatus string

const (
	// InvoiceStatusDraft is a draft invoice, Xero's default for a new invoice.
	InvoiceStatusDraft InvoiceStatus = "DRAFT"
	// InvoiceStatusSubmitted is an invoice awaiting approval.
	InvoiceStatusSubmitted InvoiceStatus = "SUBMITTED"
	// InvoiceStatusDeleted is a deleted draft or submitted invoice.
	InvoiceStatusDeleted InvoiceStatus = "DELETED"
	// InvoiceStatusAuthorised is an approved invoice awaiting payment or partly paid; payments apply only to it.
	InvoiceStatusAuthorised InvoiceStatus = "AUTHORISED"
	// InvoiceStatusPaid is a fully paid invoice; Xero sets it when the amount due reaches zero.
	InvoiceStatusPaid InvoiceStatus = "PAID"
	// InvoiceStatusVoided is a voided approved invoice.
	InvoiceStatusVoided InvoiceStatus = "VOIDED"
)

// LineAmountTypes is Xero's own value for whether line amounts include tax.
type LineAmountTypes string

const (
	// LineAmountTypesExclusive means line amounts exclude tax, Xero's default.
	LineAmountTypesExclusive LineAmountTypes = "Exclusive"
	// LineAmountTypesInclusive means line amounts include tax.
	LineAmountTypesInclusive LineAmountTypes = "Inclusive"
	// LineAmountTypesNoTax means lines have no tax.
	LineAmountTypesNoTax LineAmountTypes = "NoTax"
)

// InvoiceStatuses returns Xero's invoice statuses in the order Xero documents them.
func InvoiceStatuses() []InvoiceStatus {
	return []InvoiceStatus{
		InvoiceStatusDraft, InvoiceStatusSubmitted, InvoiceStatusDeleted, InvoiceStatusAuthorised, InvoiceStatusPaid, InvoiceStatusVoided,
	}
}

// Invoice is one Xero sales invoice or bill. Every amount is an exact Decimal in the invoice's
// currency, and Type, Status, and LineAmountTypes keep Xero's values.
type Invoice struct {
	// InvoiceID is Xero's stable invoice identifier, a UUID.
	InvoiceID string `json:"invoiceId"`
	// InvoiceNumber is the document number: unique for ACCREC, assigned from the organisation's
	// invoice settings when none was given, and the supplier's non-unique number for ACCPAY.
	InvoiceNumber string `json:"invoiceNumber,omitempty"`
	// Type is ACCREC or ACCPAY.
	Type InvoiceType `json:"type"`
	// Status is DRAFT, SUBMITTED, DELETED, AUTHORISED, PAID, or VOIDED.
	Status InvoiceStatus `json:"status"`
	// Contact is the invoiced customer or the billing supplier.
	Contact InvoiceContact `json:"contact"`
	// Date is the issue date, YYYY-MM-DD.
	Date string `json:"date,omitempty"`
	// DueDate is the due date, YYYY-MM-DD, when set.
	DueDate string `json:"dueDate,omitempty"`
	// Reference is the additional reference, when set.
	Reference string `json:"reference,omitempty"`
	// CurrencyCode is the ISO 4217 currency the invoice was raised in.
	CurrencyCode string `json:"currencyCode,omitempty"`
	// CurrencyRate is 1 for the base currency, or the rate Xero applied.
	CurrencyRate Decimal `json:"currencyRate,omitempty"`
	// LineAmountTypes is Exclusive, Inclusive, or NoTax.
	LineAmountTypes LineAmountTypes `json:"lineAmountTypes,omitempty"`
	// LineItems are the invoice lines; Xero returns them on a single read and on paged lists.
	LineItems []InvoiceLineItem `json:"lineItems,omitempty"`
	// SubTotal is the total excluding tax.
	SubTotal Decimal `json:"subTotal,omitempty"`
	// TotalTax is the tax total.
	TotalTax Decimal `json:"totalTax,omitempty"`
	// Total is SubTotal plus TotalTax plus any rounding.
	Total Decimal `json:"total,omitempty"`
	// AmountDue is the amount still to be paid.
	AmountDue Decimal `json:"amountDue,omitempty"`
	// AmountPaid is the sum of payments.
	AmountPaid Decimal `json:"amountPaid,omitempty"`
	// AmountCredited is the sum of credit notes, overpayments, and prepayments applied.
	AmountCredited Decimal `json:"amountCredited,omitempty"`
	// Payments are the payments applied to the invoice.
	Payments []InvoicePayment `json:"payments,omitempty"`
	// FullyPaidOnDate is the date the invoice became fully paid, YYYY-MM-DD, when it is.
	FullyPaidOnDate string `json:"fullyPaidOnDate,omitempty"`
	// IsSentToContact reports that Xero shows the invoice as sent.
	IsSentToContact bool `json:"isSentToContact,omitempty"`
	// UpdatedAt is Xero's last-modified instant in UTC, or zero when Xero omits it.
	UpdatedAt time.Time `json:"updatedAt"`
}

// InvoiceContact identifies the contact on an invoice.
type InvoiceContact struct {
	// ContactID is Xero's contact UUID.
	ContactID string `json:"contactId"`
	// Name is the contact name Xero returned.
	Name string `json:"name,omitempty"`
}

// InvoiceLineItem is one invoice line as Xero stores it.
type InvoiceLineItem struct {
	// LineItemID is Xero's line identifier.
	LineItemID string `json:"lineItemId,omitempty"`
	// Description is the line description.
	Description string `json:"description,omitempty"`
	// Quantity is the line quantity, up to four decimal places.
	Quantity Decimal `json:"quantity,omitempty"`
	// UnitAmount is the unit price, read with four decimal places.
	UnitAmount Decimal `json:"unitAmount,omitempty"`
	// ItemCode is the inventory item code, when set.
	ItemCode string `json:"itemCode,omitempty"`
	// AccountCode is the chart-of-accounts code, when set.
	AccountCode string `json:"accountCode,omitempty"`
	// TaxType is the Xero tax type, when set.
	TaxType string `json:"taxType,omitempty"`
	// TaxAmount is the line tax Xero calculated or was given.
	TaxAmount Decimal `json:"taxAmount,omitempty"`
	// LineAmount is the line total after any discount.
	LineAmount Decimal `json:"lineAmount,omitempty"`
	// DiscountRate is the percentage discount on an ACCREC line, when set.
	DiscountRate Decimal `json:"discountRate,omitempty"`
}

// InvoicePayment is one payment Xero lists on an invoice.
type InvoicePayment struct {
	// PaymentID is Xero's payment UUID.
	PaymentID string `json:"paymentId"`
	// Date is the payment date, YYYY-MM-DD.
	Date string `json:"date,omitempty"`
	// Amount is the payment amount in the invoice's currency.
	Amount Decimal `json:"amount,omitempty"`
}

// InvoicePage is one bounded page of invoices.
type InvoicePage struct {
	// Invoices are the invoices on this page in Xero's order, by last modification then ID.
	Invoices []Invoice `json:"invoices"`
	// Page is the 1-based page number that was read.
	Page int `json:"page"`
	// PageSize is the requested page size.
	PageSize int `json:"pageSize"`
	// PageCount is Xero's total page count, or zero when Xero omits pagination.
	PageCount int `json:"pageCount,omitempty"`
	// ItemCount is Xero's total number of matching invoices, or zero when Xero omits pagination.
	ItemCount int `json:"itemCount,omitempty"`
	// HasMorePages reports that Page+1 may hold more invoices.
	HasMorePages bool `json:"hasMorePages"`
}

// invoiceWire is the subset of a Xero Invoice element the connector reads.
type invoiceWire struct {
	InvoiceID        string            `json:"InvoiceID"`
	InvoiceNumber    string            `json:"InvoiceNumber"`
	Type             string            `json:"Type"`
	Status           string            `json:"Status"`
	Contact          contactWire       `json:"Contact"`
	Date             string            `json:"Date"`
	DateString       string            `json:"DateString"`
	DueDate          string            `json:"DueDate"`
	DueDateString    string            `json:"DueDateString"`
	Reference        string            `json:"Reference"`
	CurrencyCode     string            `json:"CurrencyCode"`
	CurrencyRate     wireDecimal       `json:"CurrencyRate"`
	LineAmountTypes  string            `json:"LineAmountTypes"`
	LineItems        []lineItemWire    `json:"LineItems"`
	SubTotal         wireDecimal       `json:"SubTotal"`
	TotalTax         wireDecimal       `json:"TotalTax"`
	Total            wireDecimal       `json:"Total"`
	AmountDue        wireDecimal       `json:"AmountDue"`
	AmountPaid       wireDecimal       `json:"AmountPaid"`
	AmountCredited   wireDecimal       `json:"AmountCredited"`
	Payments         []paymentListWire `json:"Payments"`
	FullyPaidOnDate  string            `json:"FullyPaidOnDate"`
	SentToContact    wireBool          `json:"SentToContact"`
	UpdatedDateUTC   string            `json:"UpdatedDateUTC"`
	HasErrors        wireBool          `json:"HasErrors"`
	ValidationErrors []validationWire  `json:"ValidationErrors"`
	Warnings         []validationWire  `json:"Warnings"`
}

type lineItemWire struct {
	LineItemID   string      `json:"LineItemID"`
	Description  string      `json:"Description"`
	Quantity     wireDecimal `json:"Quantity"`
	UnitAmount   wireDecimal `json:"UnitAmount"`
	ItemCode     string      `json:"ItemCode"`
	AccountCode  string      `json:"AccountCode"`
	TaxType      string      `json:"TaxType"`
	TaxAmount    wireDecimal `json:"TaxAmount"`
	LineAmount   wireDecimal `json:"LineAmount"`
	DiscountRate wireDecimal `json:"DiscountRate"`
}

// paymentListWire is a payment summary inside an invoice.
type paymentListWire struct {
	PaymentID  string      `json:"PaymentID"`
	Date       string      `json:"Date"`
	DateString string      `json:"DateString"`
	Amount     wireDecimal `json:"Amount"`
}

// validationWire is a Xero validation error or warning; only its presence is counted, never its text.
type validationWire = json.RawMessage

// invoicePageWire is a GET Invoices response.
type invoicePageWire struct {
	Pagination *pageWire      `json:"pagination"`
	Invoices   *[]invoiceWire `json:"Invoices"`
}

func decodeInvoice(wire invoiceWire) (Invoice, error) {
	if !uuidPattern.MatchString(wire.InvoiceID) {
		return Invoice{}, errors.New("invoice has no InvoiceID")
	}
	if wire.Type == "" || wire.Status == "" {
		return Invoice{}, errors.New("invoice has no Type or Status")
	}
	date, err := calendarDateFromWire(wire.DateString, wire.Date)
	if err != nil {
		return Invoice{}, errors.New("invoice has an invalid Date")
	}
	dueDate, err := calendarDateFromWire(wire.DueDateString, wire.DueDate)
	if err != nil {
		return Invoice{}, errors.New("invoice has an invalid DueDate")
	}
	fullyPaidOnDate, err := calendarDateFromWire("", wire.FullyPaidOnDate)
	if err != nil {
		return Invoice{}, errors.New("invoice has an invalid FullyPaidOnDate")
	}
	updatedAt, err := timestampFromWire(wire.UpdatedDateUTC)
	if err != nil {
		return Invoice{}, errors.New("invoice has an invalid UpdatedDateUTC")
	}
	invoice := Invoice{
		InvoiceID: strings.ToLower(wire.InvoiceID), InvoiceNumber: wire.InvoiceNumber,
		Type: InvoiceType(wire.Type), Status: InvoiceStatus(wire.Status),
		Contact: InvoiceContact{ContactID: strings.ToLower(wire.Contact.ContactID), Name: wire.Contact.Name},
		Date:    date, DueDate: dueDate, Reference: wire.Reference, CurrencyCode: wire.CurrencyCode,
		CurrencyRate: wire.CurrencyRate.value, LineAmountTypes: LineAmountTypes(wire.LineAmountTypes),
		SubTotal: wire.SubTotal.value, TotalTax: wire.TotalTax.value, Total: wire.Total.value,
		AmountDue: wire.AmountDue.value, AmountPaid: wire.AmountPaid.value, AmountCredited: wire.AmountCredited.value,
		FullyPaidOnDate: fullyPaidOnDate, IsSentToContact: bool(wire.SentToContact), UpdatedAt: updatedAt,
	}
	for _, line := range wire.LineItems {
		invoice.LineItems = append(invoice.LineItems, InvoiceLineItem{
			LineItemID: line.LineItemID, Description: line.Description, Quantity: line.Quantity.value, UnitAmount: line.UnitAmount.value,
			ItemCode: line.ItemCode, AccountCode: line.AccountCode, TaxType: line.TaxType, TaxAmount: line.TaxAmount.value,
			LineAmount: line.LineAmount.value, DiscountRate: line.DiscountRate.value,
		})
	}
	for index, payment := range wire.Payments {
		paymentDate, err := calendarDateFromWire(payment.DateString, payment.Date)
		if err != nil || !uuidPattern.MatchString(payment.PaymentID) {
			return Invoice{}, fmt.Errorf("invoice payment %d is invalid", index)
		}
		invoice.Payments = append(invoice.Payments, InvoicePayment{
			PaymentID: strings.ToLower(payment.PaymentID), Date: paymentDate, Amount: payment.Amount.value,
		})
	}
	return invoice, nil
}

// rejectsInvoiceElement reports a per-invoice error, which Xero returns with HTTP 200 only when summarizeErrors is false.
func rejectsInvoiceElement(wire invoiceWire) bool {
	return bool(wire.HasErrors) || len(wire.ValidationErrors) != 0
}
