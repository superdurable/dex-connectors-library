// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// EmailStatus is QuickBooks's own invoice email state.
type EmailStatus string

const (
	// EmailStatusNotSet means no email has been requested for the invoice.
	EmailStatusNotSet EmailStatus = "NotSet"
	// EmailStatusNeedToSend means the invoice is marked to be emailed but has not been sent.
	EmailStatusNeedToSend EmailStatus = "NeedToSend"
	// EmailStatusEmailSent means QuickBooks has emailed the invoice.
	EmailStatusEmailSent EmailStatus = "EmailSent"
)

// LineDetailType is QuickBooks's own DetailType of an invoice line, passed through unchanged.
type LineDetailType string

const (
	// LineDetailTypeSalesItem is a sales line for one product or service item.
	LineDetailTypeSalesItem LineDetailType = "SalesItemLineDetail"
	// LineDetailTypeSubTotal is the subtotal line QuickBooks adds to every invoice it returns.
	LineDetailTypeSubTotal LineDetailType = "SubTotalLineDetail"
	// LineDetailTypeDiscount is a discount line.
	LineDetailTypeDiscount LineDetailType = "DiscountLineDetail"
	// LineDetailTypeGroup is a bundle of item lines.
	LineDetailTypeGroup LineDetailType = "GroupLineDetail"
	// LineDetailTypeDescriptionOnly is a line with text and no amount.
	LineDetailTypeDescriptionOnly LineDetailType = "DescriptionOnly"
)

// Invoice is one QuickBooks Online invoice. QuickBooks has no invoice status field: Balance is the
// amount still open, and an invoice is paid when Balance is zero. Amounts are exact Decimals in
// the invoice's currency.
type Invoice struct {
	// InvoiceID is QuickBooks's Id for the invoice, a decimal string such as 130.
	InvoiceID string `json:"invoiceId"`
	// SyncToken is the version QuickBooks requires for an update of this invoice.
	SyncToken string `json:"syncToken,omitempty"`
	// DocNumber is the invoice number, when the company assigns or accepts one.
	DocNumber string `json:"docNumber,omitempty"`
	// CustomerID is the invoiced customer's Id.
	CustomerID string `json:"customerId"`
	// CustomerName is the customer's display name as QuickBooks returns it.
	CustomerName string `json:"customerName,omitempty"`
	// TxnDate is the invoice date, YYYY-MM-DD.
	TxnDate string `json:"txnDate,omitempty"`
	// DueDate is the payment due date, YYYY-MM-DD.
	DueDate string `json:"dueDate,omitempty"`
	// CurrencyCode is the invoice's ISO 4217 currency, when QuickBooks returns it.
	CurrencyCode string `json:"currencyCode,omitempty"`
	// ExchangeRate is the home-currency rate of a foreign-currency invoice, when returned.
	ExchangeRate Decimal `json:"exchangeRate,omitempty"`
	// Lines are the invoice lines in QuickBooks's order, including the SubTotalLineDetail line.
	Lines []InvoiceLine `json:"lines"`
	// TotalAmount is QuickBooks's TotalAmt, the invoice total including tax.
	TotalAmount Decimal `json:"totalAmount"`
	// TotalTax is the tax QuickBooks calculated, when returned.
	TotalTax Decimal `json:"totalTax,omitempty"`
	// Balance is the amount still due; zero means the invoice is paid.
	Balance Decimal `json:"balance"`
	// EmailStatus is NotSet, NeedToSend, or EmailSent, or a newer QuickBooks value.
	EmailStatus EmailStatus `json:"emailStatus,omitempty"`
	// BillEmail is the address QuickBooks sends the invoice to, when set.
	BillEmail string `json:"billEmail,omitempty"`
	// DeliveredAt is when QuickBooks last delivered the invoice, in UTC, when it has.
	DeliveredAt *time.Time `json:"deliveredAt,omitempty"`
	// CustomerMemo is the message shown to the customer, when set.
	CustomerMemo string `json:"customerMemo,omitempty"`
	// PrivateNote is the internal memo, when set.
	PrivateNote string `json:"privateNote,omitempty"`
	// LinkedPaymentIDs are the payments QuickBooks lists against the invoice.
	LinkedPaymentIDs []string `json:"linkedPaymentIds,omitempty"`
	// CreatedAt is when the invoice was created, in UTC.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	// UpdatedAt is when the invoice last changed, in UTC.
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// InvoiceLine is one invoice line. Item fields are set only on a SalesItemLineDetail line.
type InvoiceLine struct {
	// LineID is QuickBooks's Id for the line, when returned.
	LineID string `json:"lineId,omitempty"`
	// DetailType is QuickBooks's own line type, such as SalesItemLineDetail.
	DetailType LineDetailType `json:"detailType"`
	// Description is the line text, when set.
	Description string `json:"description,omitempty"`
	// Amount is the line amount.
	Amount Decimal `json:"amount,omitempty"`
	// ItemID is the product or service item's Id.
	ItemID string `json:"itemId,omitempty"`
	// ItemName is the item's name as QuickBooks returns it.
	ItemName string `json:"itemName,omitempty"`
	// Quantity is the item quantity, when set.
	Quantity Decimal `json:"quantity,omitempty"`
	// UnitPrice is the item's unit price, when set.
	UnitPrice Decimal `json:"unitPrice,omitempty"`
	// TaxCodeID is the line's tax code, such as TAX or NON in a US company, when set.
	TaxCodeID string `json:"taxCodeId,omitempty"`
	// ServiceDate is the date of service, YYYY-MM-DD, when set.
	ServiceDate string `json:"serviceDate,omitempty"`
}

type invoiceWire struct {
	ID           string            `json:"Id"`
	SyncToken    string            `json:"SyncToken"`
	DocNumber    string            `json:"DocNumber"`
	CustomerRef  *referenceWire    `json:"CustomerRef"`
	TxnDate      string            `json:"TxnDate"`
	DueDate      string            `json:"DueDate"`
	CurrencyRef  *referenceWire    `json:"CurrencyRef"`
	ExchangeRate wireDecimal       `json:"ExchangeRate"`
	Line         []invoiceLineWire `json:"Line"`
	TotalAmt     wireDecimal       `json:"TotalAmt"`
	Balance      wireDecimal       `json:"Balance"`
	TxnTaxDetail *struct {
		TotalTax wireDecimal `json:"TotalTax"`
	} `json:"TxnTaxDetail"`
	EmailStatus  string            `json:"EmailStatus"`
	BillEmail    *emailAddressWire `json:"BillEmail"`
	DeliveryInfo *struct {
		DeliveryTime string `json:"DeliveryTime"`
	} `json:"DeliveryInfo"`
	CustomerMemo *struct {
		Value string `json:"value"`
	} `json:"CustomerMemo"`
	PrivateNote string          `json:"PrivateNote"`
	LinkedTxn   []linkedTxnWire `json:"LinkedTxn"`
	MetaData    *metadataWire   `json:"MetaData"`
}

type invoiceLineWire struct {
	ID                  string      `json:"Id"`
	DetailType          string      `json:"DetailType"`
	Description         string      `json:"Description"`
	Amount              wireDecimal `json:"Amount"`
	SalesItemLineDetail *struct {
		ItemRef     *referenceWire `json:"ItemRef"`
		Qty         wireDecimal    `json:"Qty"`
		UnitPrice   wireDecimal    `json:"UnitPrice"`
		TaxCodeRef  *referenceWire `json:"TaxCodeRef"`
		ServiceDate string         `json:"ServiceDate"`
	} `json:"SalesItemLineDetail"`
}

// linkedTxnWire is QuickBooks's LinkedTxn, a reference from one transaction to another.
type linkedTxnWire struct {
	TxnID   string `json:"TxnId"`
	TxnType string `json:"TxnType"`
}

// decodeInvoice validates one invoice element and keeps QuickBooks's exact amounts.
func decodeInvoice(wire invoiceWire) (Invoice, error) {
	if !entityIDPattern.MatchString(wire.ID) {
		return Invoice{}, errors.New("invoice has no valid Id")
	}
	if wire.CustomerRef == nil || !entityIDPattern.MatchString(wire.CustomerRef.Value) {
		return Invoice{}, errors.New("invoice has no valid CustomerRef")
	}
	if !wire.TotalAmt.isPresent || !wire.Balance.isPresent {
		return Invoice{}, errors.New("invoice has no TotalAmt or Balance")
	}
	for _, date := range []struct{ name, value string }{{"TxnDate", wire.TxnDate}, {"DueDate", wire.DueDate}} {
		if date.value != "" && validateCalendarDate(date.name, date.value) != nil {
			return Invoice{}, fmt.Errorf("invoice has an invalid %s", date.name)
		}
	}
	invoice := Invoice{
		InvoiceID: wire.ID, SyncToken: wire.SyncToken, DocNumber: wire.DocNumber, CustomerID: wire.CustomerRef.Value,
		CustomerName: wire.CustomerRef.Name, TxnDate: wire.TxnDate, DueDate: wire.DueDate, ExchangeRate: wire.ExchangeRate.value,
		TotalAmount: wire.TotalAmt.value, Balance: wire.Balance.value, EmailStatus: EmailStatus(wire.EmailStatus),
		PrivateNote: wire.PrivateNote, Lines: make([]InvoiceLine, 0, len(wire.Line)),
	}
	if wire.CurrencyRef != nil {
		invoice.CurrencyCode = wire.CurrencyRef.Value
	}
	if wire.TxnTaxDetail != nil {
		invoice.TotalTax = wire.TxnTaxDetail.TotalTax.value
	}
	if wire.BillEmail != nil {
		invoice.BillEmail = strings.TrimSpace(wire.BillEmail.Address)
	}
	if wire.CustomerMemo != nil {
		invoice.CustomerMemo = wire.CustomerMemo.Value
	}
	if wire.DeliveryInfo != nil && wire.DeliveryInfo.DeliveryTime != "" {
		deliveredAt, err := parseOptionalTimestamp(wire.DeliveryInfo.DeliveryTime)
		if err != nil {
			return Invoice{}, errors.New("invoice has an invalid DeliveryInfo.DeliveryTime")
		}
		invoice.DeliveredAt = deliveredAt
	}
	for index, line := range wire.Line {
		decoded, err := decodeInvoiceLine(line)
		if err != nil {
			return Invoice{}, fmt.Errorf("invoice line %d %w", index, err)
		}
		invoice.Lines = append(invoice.Lines, decoded)
	}
	for _, linked := range wire.LinkedTxn {
		if linked.TxnType == "Payment" && entityIDPattern.MatchString(linked.TxnID) {
			invoice.LinkedPaymentIDs = append(invoice.LinkedPaymentIDs, linked.TxnID)
		}
	}
	createdAt, updatedAt, err := decodeMetadataTimes(wire.MetaData)
	if err != nil {
		return Invoice{}, fmt.Errorf("invoice %w", err)
	}
	invoice.CreatedAt, invoice.UpdatedAt = createdAt, updatedAt
	return invoice, nil
}

func decodeInvoiceLine(wire invoiceLineWire) (InvoiceLine, error) {
	if wire.DetailType == "" {
		return InvoiceLine{}, errors.New("has no DetailType")
	}
	line := InvoiceLine{LineID: wire.ID, DetailType: LineDetailType(wire.DetailType), Description: wire.Description, Amount: wire.Amount.value}
	if detail := wire.SalesItemLineDetail; detail != nil {
		if detail.ItemRef != nil {
			line.ItemID, line.ItemName = detail.ItemRef.Value, detail.ItemRef.Name
		}
		if detail.TaxCodeRef != nil {
			line.TaxCodeID = detail.TaxCodeRef.Value
		}
		line.Quantity, line.UnitPrice, line.ServiceDate = detail.Qty.value, detail.UnitPrice.value, detail.ServiceDate
	}
	return line, nil
}
