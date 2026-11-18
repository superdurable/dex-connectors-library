// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validRecordPaymentInput() xero.RecordPaymentInput {
	return xero.RecordPaymentInput{
		InvoiceID: strings.ToUpper(testInvoiceID), AccountCode: " 090 ", Date: "2026-10-01", Amount: "109.89",
		Reference: "ch_3QxF2a", IsReconciled: true,
	}
}

func paymentJSON(invoiceID string) map[string]any {
	return map[string]any{
		"PaymentID": testPaymentID, "Date": "/Date(1759276800000+0000)/", "Amount": 109.89, "BankAmount": 109.89,
		"CurrencyRate": 1.000000, "Reference": "ch_3QxF2a", "Status": "AUTHORISED", "PaymentType": "ACCRECPAYMENT",
		"IsReconciled": true, "UpdatedDateUTC": "/Date(1759309200000+0000)/",
		"Account": map[string]any{"AccountID": "AC993F75-035B-433C-82E0-7B7A2D40802C", "Code": "090"},
		"Invoice": map[string]any{
			"InvoiceID": invoiceID, "InvoiceNumber": "INV-0042", "Type": "ACCREC", "Status": "PAID",
			"AmountDue": 0.00, "AmountPaid": 109.89, "CurrencyCode": "USD",
		},
	}
}

func TestRecordPaymentSendsTheExactAmountUnderTheStepKey(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"Status": "OK", "Payments": []any{paymentJSON(testInvoiceID)}})
	})
	result, err := sdkgo.RunMutation(newXeroDexContext("pay"), newXeroClient(t, provider.URL).RecordPayment(), xeroConnection, validRecordPaymentInput())
	require.NoError(t, err)
	require.Equal(t, xero.RecordPaymentBranchRecorded, result.Branch)
	payment := result.Value.Payment
	require.Equal(t, testPaymentID, payment.PaymentID)
	require.Equal(t, xero.Decimal("109.89"), payment.Amount)
	require.Equal(t, xero.Decimal("1"), payment.CurrencyRate, "the JSON number Go encoded in the fake is kept as written")
	require.Equal(t, xero.InvoiceStatusPaid, payment.Invoice.Status)
	require.Equal(t, xero.Decimal("0"), payment.Invoice.AmountDue)
	require.Equal(t, "ac993f75-035b-433c-82e0-7b7a2d40802c", payment.Account.AccountID)
	require.Equal(t, "2025-10-01", payment.Date)
	require.Equal(t, testInvoiceID, result.Value.InvoiceID)
	require.Equal(t, testPaymentID, result.Receipt.ProviderObjectID)

	request := provider.request(0)
	require.Equal(t, http.MethodPut, request.method)
	require.Equal(t, "/api.xro/2.0/Payments", request.path)
	require.Equal(t, string(result.Receipt.IdempotencyKey), request.header.Get("Idempotency-Key"))
	require.Equal(t, `{"Invoice":{"InvoiceID":"`+testInvoiceID+`"},"Account":{"Code":"090"},"Date":"2026-10-01","Amount":109.89,`+
		`"Reference":"ch_3QxF2a","IsReconciled":true}`, request.body)
}

func TestRecordPaymentRetriesAnUnknownOutcomeUnderTheKey(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter){
		"payment for another invoice": func(response http.ResponseWriter) {
			writeValue(t, response, http.StatusOK, map[string]any{"Payments": []any{paymentJSON(otherContactID)}})
		},
		"no payment ID": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"Payments":[{"Amount":109.89}]}`)
		},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { reply(response) })
			_, err := sdkgo.RunMutation(newXeroDexContext("pay-"+name), newXeroClient(t, provider.URL).RecordPayment(), xeroConnection, validRecordPaymentInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "the Idempotency-Key makes the identical resend safe")
			require.Equal(t, 1, provider.requestCount())
			require.NotEmpty(t, provider.request(0).header.Get("Idempotency-Key"))
		})
	}
}

func TestRecordPaymentSelectsUncertainForACachedInternalError(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusInternalServerError, `{}`)
	})
	result, err := sdkgo.RunMutation(newXeroDexContext("pay-cached-500"), newXeroClient(t, provider.URL).RecordPayment(), xeroConnection, validRecordPaymentInput())
	require.NoError(t, err)
	require.Equal(t, xero.RecordPaymentBranchUncertain, result.Branch, "Xero replays the cached 500 to every resend under the key")
	require.Equal(t, sdkgo.FailureAvailability, result.Failure.Kind)
	require.Equal(t, 1, provider.requestCount())
}

func TestRecordPaymentRejectsInexactOrAmbiguousInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusInternalServerError, `{}`)
	})
	client := newXeroClient(t, provider.URL)
	for name, change := range map[string]func(*xero.RecordPaymentInput){
		"three decimal places":  func(input *xero.RecordPaymentInput) { input.Amount = "109.891" },
		"zero amount":           func(input *xero.RecordPaymentInput) { input.Amount = "0.00" },
		"negative amount":       func(input *xero.RecordPaymentInput) { input.Amount = "-5" },
		"float formatting":      func(input *xero.RecordPaymentInput) { input.Amount = "1.0989e2" },
		"both accounts":         func(input *xero.RecordPaymentInput) { input.AccountID = "ac993f75-035b-433c-82e0-7b7a2d40802c" },
		"no account":            func(input *xero.RecordPaymentInput) { input.AccountCode = "" },
		"missing date":          func(input *xero.RecordPaymentInput) { input.Date = "" },
		"invoice number for ID": func(input *xero.RecordPaymentInput) { input.InvoiceID = "INV-0042" },
		"inexact bank amount":   func(input *xero.RecordPaymentInput) { input.BankAmount = "12.345" },
	} {
		t.Run(name, func(t *testing.T) {
			input := validRecordPaymentInput()
			change(&input)
			result, err := sdkgo.RunMutation(newXeroDexContext("invalid-"+name), client.RecordPayment(), xeroConnection, input)
			require.NoError(t, err)
			require.Equal(t, xero.RecordPaymentBranchDefect, result.Branch)
		})
	}
	require.Zero(t, provider.requestCount())
}
