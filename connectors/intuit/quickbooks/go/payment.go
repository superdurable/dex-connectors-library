// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"errors"
	"fmt"
	"time"
)

// Payment is one QuickBooks Online received payment. Amounts are exact Decimals in the payment's currency.
type Payment struct {
	// PaymentID is QuickBooks's Id for the payment, a decimal string such as 186.
	PaymentID string `json:"paymentId"`
	// SyncToken is the version QuickBooks requires for an update of this payment.
	SyncToken string `json:"syncToken,omitempty"`
	// CustomerID is the paying customer's Id.
	CustomerID string `json:"customerId"`
	// TxnDate is the payment date, YYYY-MM-DD.
	TxnDate string `json:"txnDate,omitempty"`
	// TotalAmount is QuickBooks's TotalAmt, the amount received.
	TotalAmount Decimal `json:"totalAmount"`
	// UnappliedAmount is the part of the payment not applied to any invoice, when returned.
	UnappliedAmount Decimal `json:"unappliedAmount,omitempty"`
	// CurrencyCode is the payment's ISO 4217 currency, when QuickBooks returns it.
	CurrencyCode string `json:"currencyCode,omitempty"`
	// PaymentReferenceNumber is QuickBooks's PaymentRefNum, such as a check number, when set.
	PaymentReferenceNumber string `json:"paymentReferenceNumber,omitempty"`
	// DepositAccountID is the account the payment was deposited to, when returned.
	DepositAccountID string `json:"depositAccountId,omitempty"`
	// AppliedInvoices are the invoices the payment pays and the amount applied to each.
	AppliedInvoices []PaymentApplication `json:"appliedInvoices,omitempty"`
	// PrivateNote is the internal memo, when set.
	PrivateNote string `json:"privateNote,omitempty"`
	// CreatedAt is when the payment was created, in UTC.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	// UpdatedAt is when the payment last changed, in UTC.
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// PaymentApplication is the part of a payment applied to one invoice.
type PaymentApplication struct {
	// InvoiceID is the paid invoice's Id.
	InvoiceID string `json:"invoiceId"`
	// Amount is the amount applied to the invoice.
	Amount Decimal `json:"amount"`
}

type paymentWire struct {
	ID                  string            `json:"Id"`
	SyncToken           string            `json:"SyncToken"`
	CustomerRef         *referenceWire    `json:"CustomerRef"`
	TxnDate             string            `json:"TxnDate"`
	TotalAmt            wireDecimal       `json:"TotalAmt"`
	UnappliedAmt        wireDecimal       `json:"UnappliedAmt"`
	CurrencyRef         *referenceWire    `json:"CurrencyRef"`
	PaymentRefNum       string            `json:"PaymentRefNum"`
	DepositToAccountRef *referenceWire    `json:"DepositToAccountRef"`
	Line                []paymentLineWire `json:"Line"`
	PrivateNote         string            `json:"PrivateNote"`
	MetaData            *metadataWire     `json:"MetaData"`
}

type paymentLineWire struct {
	Amount    wireDecimal     `json:"Amount"`
	LinkedTxn []linkedTxnWire `json:"LinkedTxn"`
}

// decodePayment validates one payment element and keeps QuickBooks's exact amounts.
func decodePayment(wire paymentWire) (Payment, error) {
	if !entityIDPattern.MatchString(wire.ID) {
		return Payment{}, errors.New("payment has no valid Id")
	}
	if wire.CustomerRef == nil || !entityIDPattern.MatchString(wire.CustomerRef.Value) {
		return Payment{}, errors.New("payment has no valid CustomerRef")
	}
	if !wire.TotalAmt.isPresent {
		return Payment{}, errors.New("payment has no TotalAmt")
	}
	if wire.TxnDate != "" && validateCalendarDate("TxnDate", wire.TxnDate) != nil {
		return Payment{}, errors.New("payment has an invalid TxnDate")
	}
	payment := Payment{
		PaymentID: wire.ID, SyncToken: wire.SyncToken, CustomerID: wire.CustomerRef.Value, TxnDate: wire.TxnDate,
		TotalAmount: wire.TotalAmt.value, UnappliedAmount: wire.UnappliedAmt.value, PaymentReferenceNumber: wire.PaymentRefNum,
		PrivateNote: wire.PrivateNote,
	}
	if wire.CurrencyRef != nil {
		payment.CurrencyCode = wire.CurrencyRef.Value
	}
	if wire.DepositToAccountRef != nil {
		payment.DepositAccountID = wire.DepositToAccountRef.Value
	}
	for index, line := range wire.Line {
		for _, linked := range line.LinkedTxn {
			if linked.TxnType != "Invoice" {
				continue
			}
			if !entityIDPattern.MatchString(linked.TxnID) || !line.Amount.isPresent {
				return Payment{}, fmt.Errorf("payment line %d links an invoice without a valid TxnId or Amount", index)
			}
			payment.AppliedInvoices = append(payment.AppliedInvoices, PaymentApplication{InvoiceID: linked.TxnID, Amount: line.Amount.value})
		}
	}
	createdAt, updatedAt, err := decodeMetadataTimes(wire.MetaData)
	if err != nil {
		return Payment{}, fmt.Errorf("payment %w", err)
	}
	payment.CreatedAt, payment.UpdatedAt = createdAt, updatedAt
	return payment, nil
}
