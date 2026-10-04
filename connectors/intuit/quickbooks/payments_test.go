// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validRecordPaymentInput() quickbooks.RecordPaymentInput {
	return quickbooks.RecordPaymentInput{
		CustomerID: testCustomerID, InvoiceID: testInvoiceID, Amount: "99.90", TxnDate: "2026-10-01",
		PaymentReferenceNumber: "ch_3QxF2a", DepositAccountID: "35", PaymentMethodID: "4", PrivateNote: "Card payment",
	}
}

func paymentJSON(invoiceID string, amount string) map[string]any {
	return map[string]any{
		"Id": testPaymentID, "SyncToken": "0", "CustomerRef": map[string]any{"value": testCustomerID, "name": "City Agency"},
		"TotalAmt": json.Number(amount), "UnappliedAmt": json.Number("0"), "TxnDate": "2026-10-01", "PaymentRefNum": "ch_3QxF2a",
		"DepositToAccountRef": map[string]any{"value": "35"}, "CurrencyRef": map[string]any{"value": "USD"}, "PrivateNote": "Card payment",
		"Line":     []any{map[string]any{"Amount": json.Number(amount), "LinkedTxn": []any{map[string]any{"TxnId": invoiceID, "TxnType": "Invoice"}}}},
		"MetaData": map[string]any{"CreateTime": "2026-10-01T02:03:00-07:00", "LastUpdatedTime": "2026-10-01T02:03:00-07:00"},
	}
}

func TestRecordPaymentAppliesTheExactAmountToTheInvoice(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"Payment": paymentJSON(testInvoiceID, "99.90")})
	})
	result, err := sdkgo.RunMutation(newQuickBooksDexContext("pay"), newQuickBooksClient(t, provider.URL).RecordPayment(), quickbooksConnection,
		validRecordPaymentInput())
	require.NoError(t, err)
	require.Equal(t, quickbooks.RecordPaymentBranchRecorded, result.Branch)
	payment := result.Value
	require.Equal(t, testPaymentID, payment.PaymentID)
	require.Equal(t, quickbooks.Decimal("99.90"), payment.TotalAmount)
	require.Equal(t, []quickbooks.PaymentApplication{{InvoiceID: testInvoiceID, Amount: "99.90"}}, payment.AppliedInvoices)
	require.Equal(t, "35", payment.DepositAccountID)
	require.Equal(t, testPaymentID, result.Receipt.ProviderObjectID)

	request := provider.request(0)
	require.Equal(t, companyPath+"/payment", request.path)
	require.Equal(t, string(result.Receipt.IdempotencyKey), request.query.Get("requestid"))
	require.JSONEq(t, `{"CustomerRef":{"value":"58"},"TotalAmt":99.90,"TxnDate":"2026-10-01","PaymentRefNum":"ch_3QxF2a",
		"DepositToAccountRef":{"value":"35"},"PaymentMethodRef":{"value":"4"},"PrivateNote":"Card payment",
		"Line":[{"Amount":99.90,"LinkedTxn":[{"TxnId":"130","TxnType":"Invoice"}]}]}`, request.body)
	require.Contains(t, request.body, `"TotalAmt":99.90`)
}

func TestRecordPaymentValidatesBeforeAnyRequestAndDistrustsAnotherInvoice(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusBadRequest, faultJSON("ValidationFault", "6000"))
			return
		}
		writeValue(t, response, http.StatusOK, map[string]any{"Payment": paymentJSON("131", "99.90")})
	})
	client := newQuickBooksClient(t, provider.URL)
	for name, change := range map[string]func(*quickbooks.RecordPaymentInput){
		"zero":      func(input *quickbooks.RecordPaymentInput) { input.Amount = "0.00" },
		"negative":  func(input *quickbooks.RecordPaymentInput) { input.Amount = "-1" },
		"fraction":  func(input *quickbooks.RecordPaymentInput) { input.Amount = "1.005" },
		"float":     func(input *quickbooks.RecordPaymentInput) { input.Amount = "1e2" },
		"invoice":   func(input *quickbooks.RecordPaymentInput) { input.InvoiceID = "" },
		"customer":  func(input *quickbooks.RecordPaymentInput) { input.CustomerID = "cus_58" },
		"account":   func(input *quickbooks.RecordPaymentInput) { input.DepositAccountID = "Checking" },
		"reference": func(input *quickbooks.RecordPaymentInput) { input.PaymentReferenceNumber = "0123456789012345678901" },
		"date":      func(input *quickbooks.RecordPaymentInput) { input.TxnDate = "2026-13-01" },
	} {
		input := validRecordPaymentInput()
		change(&input)
		result, err := sdkgo.RunMutation(newQuickBooksDexContext(name), client.RecordPayment(), quickbooksConnection, input)
		require.NoError(t, err)
		require.Equal(t, quickbooks.RecordPaymentBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.requestCount())

	result, err := sdkgo.RunMutation(newQuickBooksDexContext("rejected"), client.RecordPayment(), quickbooksConnection, validRecordPaymentInput())
	require.NoError(t, err)
	require.Equal(t, quickbooks.RecordPaymentBranchProviderRejected, result.Branch)
	require.NotContains(t, result.Failure.Message, "SENTINEL")
	result, err = sdkgo.RunMutation(newQuickBooksDexContext("another-invoice"), client.RecordPayment(), quickbooksConnection, validRecordPaymentInput())
	require.NoError(t, err)
	require.Equal(t, quickbooks.RecordPaymentBranchUncertain, result.Branch, "a payment applied elsewhere is never reported as recorded")
}
