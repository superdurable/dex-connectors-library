// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"errors"
	"strings"
	"time"
)

// PaymentStatus is Xero's own payment status value.
type PaymentStatus string

const (
	// PaymentStatusAuthorised is a recorded payment.
	PaymentStatusAuthorised PaymentStatus = "AUTHORISED"
	// PaymentStatusDeleted is a reversed payment.
	PaymentStatusDeleted PaymentStatus = "DELETED"
)

// Payment is one Xero payment against an invoice. Amounts are exact Decimals.
type Payment struct {
	// PaymentID is Xero's stable payment identifier, a UUID.
	PaymentID string `json:"paymentId"`
	// Date is the payment date, YYYY-MM-DD.
	Date string `json:"date,omitempty"`
	// Amount is the payment in the invoice's currency.
	Amount Decimal `json:"amount"`
	// BankAmount is the payment in the account's currency, when Xero returns it.
	BankAmount Decimal `json:"bankAmount,omitempty"`
	// CurrencyRate is 1 for a base-currency payment, or the rate Xero applied.
	CurrencyRate Decimal `json:"currencyRate,omitempty"`
	// Reference is the payment's description, when set.
	Reference string `json:"reference,omitempty"`
	// Status is AUTHORISED or DELETED.
	Status PaymentStatus `json:"status"`
	// PaymentType is Xero's payment type, such as ACCRECPAYMENT for a sales invoice payment.
	PaymentType string `json:"paymentType,omitempty"`
	// IsReconciled reports that the payment is marked reconciled.
	IsReconciled bool `json:"isReconciled,omitempty"`
	// Invoice is the paid invoice as Xero reports it after the payment.
	Invoice PaymentInvoice `json:"invoice"`
	// Account is the account the payment was made from or into.
	Account PaymentAccount `json:"account"`
	// UpdatedAt is Xero's last-modified instant in UTC, or zero when Xero omits it.
	UpdatedAt time.Time `json:"updatedAt"`
}

// PaymentInvoice is the invoice summary Xero returns with a payment.
type PaymentInvoice struct {
	// InvoiceID is Xero's invoice UUID.
	InvoiceID string `json:"invoiceId"`
	// InvoiceNumber is the document number, when set.
	InvoiceNumber string `json:"invoiceNumber,omitempty"`
	// Type is ACCREC or ACCPAY, when Xero returns it.
	Type InvoiceType `json:"type,omitempty"`
	// Status is the invoice status after the payment, such as PAID, when Xero returns it.
	Status InvoiceStatus `json:"status,omitempty"`
	// AmountDue is the amount still due after the payment, when Xero returns it.
	AmountDue Decimal `json:"amountDue,omitempty"`
	// AmountPaid is the sum of payments after this one, when Xero returns it.
	AmountPaid Decimal `json:"amountPaid,omitempty"`
	// CurrencyCode is the invoice's ISO 4217 currency, when Xero returns it.
	CurrencyCode string `json:"currencyCode,omitempty"`
}

// PaymentAccount identifies the account of a payment.
type PaymentAccount struct {
	// AccountID is Xero's account UUID.
	AccountID string `json:"accountId,omitempty"`
	// Code is the account code, when the account has one.
	Code string `json:"code,omitempty"`
}

// paymentWire is the subset of a Xero Payment element the connector reads.
type paymentWire struct {
	PaymentID           string           `json:"PaymentID"`
	Date                string           `json:"Date"`
	DateString          string           `json:"DateString"`
	Amount              wireDecimal      `json:"Amount"`
	BankAmount          wireDecimal      `json:"BankAmount"`
	CurrencyRate        wireDecimal      `json:"CurrencyRate"`
	Reference           string           `json:"Reference"`
	Status              string           `json:"Status"`
	PaymentType         string           `json:"PaymentType"`
	IsReconciled        wireBool         `json:"IsReconciled"`
	UpdatedDateUTC      string           `json:"UpdatedDateUTC"`
	Invoice             paymentInvoice   `json:"Invoice"`
	Account             paymentAccount   `json:"Account"`
	HasValidationErrors wireBool         `json:"HasValidationErrors"`
	ValidationErrors    []validationWire `json:"ValidationErrors"`
	Warnings            []validationWire `json:"Warnings"`
}

type paymentInvoice struct {
	InvoiceID     string      `json:"InvoiceID"`
	InvoiceNumber string      `json:"InvoiceNumber"`
	Type          string      `json:"Type"`
	Status        string      `json:"Status"`
	AmountDue     wireDecimal `json:"AmountDue"`
	AmountPaid    wireDecimal `json:"AmountPaid"`
	CurrencyCode  string      `json:"CurrencyCode"`
}

type paymentAccount struct {
	AccountID string `json:"AccountID"`
	Code      string `json:"Code"`
}

func decodePayment(wire paymentWire) (Payment, error) {
	if !uuidPattern.MatchString(wire.PaymentID) {
		return Payment{}, errors.New("payment has no PaymentID")
	}
	if !wire.Amount.isPresent {
		return Payment{}, errors.New("payment has no Amount")
	}
	date, err := calendarDateFromWire(wire.DateString, wire.Date)
	if err != nil {
		return Payment{}, errors.New("payment has an invalid Date")
	}
	updatedAt, err := timestampFromWire(wire.UpdatedDateUTC)
	if err != nil {
		return Payment{}, errors.New("payment has an invalid UpdatedDateUTC")
	}
	return Payment{
		PaymentID: strings.ToLower(wire.PaymentID), Date: date, Amount: wire.Amount.value, BankAmount: wire.BankAmount.value,
		CurrencyRate: wire.CurrencyRate.value, Reference: wire.Reference, Status: PaymentStatus(wire.Status),
		PaymentType: wire.PaymentType, IsReconciled: bool(wire.IsReconciled), UpdatedAt: updatedAt,
		Invoice: PaymentInvoice{
			InvoiceID: strings.ToLower(wire.Invoice.InvoiceID), InvoiceNumber: wire.Invoice.InvoiceNumber,
			Type: InvoiceType(wire.Invoice.Type), Status: InvoiceStatus(wire.Invoice.Status),
			AmountDue: wire.Invoice.AmountDue.value, AmountPaid: wire.Invoice.AmountPaid.value, CurrencyCode: wire.Invoice.CurrencyCode,
		},
		Account: PaymentAccount{AccountID: strings.ToLower(wire.Account.AccountID), Code: wire.Account.Code},
	}, nil
}
