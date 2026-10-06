// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func validCreateInvoiceInput() quickbooks.CreateInvoiceInput {
	return quickbooks.CreateInvoiceInput{
		CustomerID: testCustomerID, TxnDate: "2026-10-01", DueDate: "2026-10-15", DocNumber: " ORDER-1042 ",
		BillEmail: "accounts@cityagency.example.com", CurrencyCode: "USD", CustomerMemo: "Thank you\nfor your order", PrivateNote: "Order ORDER-1042",
		Lines: []quickbooks.InvoiceLineInput{
			{ItemID: "1", Description: "Spring bouquet", Quantity: "2", UnitPrice: "49.95", TaxCodeID: "NON", ServiceDate: "2026-09-30"},
			{ItemID: "2", Description: "Card", Quantity: "3", UnitPrice: "0.33335"},
			{ItemID: "3", Description: "Discounted delivery", Amount: "-1.50"},
		},
	}
}

func TestCreateInvoiceSendsExactAmountsUnderTheRequestID(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoiceJSON(), "time": "2026-10-01T02:00:00.000-07:00"})
	})
	result, err := sdkgo.RunMutation(newQuickBooksDexContext("create-invoice"), newQuickBooksClient(t, provider.URL).CreateInvoice(), quickbooksConnection,
		validCreateInvoiceInput())
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateInvoiceBranchCreated, result.Branch)
	require.Equal(t, testInvoiceID, result.Value.Invoice.InvoiceID)
	require.Equal(t, "ORDER-1042", result.Value.DocNumber)
	require.Equal(t, testInvoiceID, result.Receipt.ProviderObjectID)

	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, companyPath+"/invoice", request.path)
	require.Equal(t, quickbooks.MinorVersion, request.query.Get("minorversion"))
	require.Equal(t, string(result.Receipt.IdempotencyKey), request.query.Get("requestid"))
	require.LessOrEqual(t, len(request.query.Get("requestid")), 50, "QuickBooks accepts a requestid of at most 50 characters")
	require.JSONEq(t, `{"CustomerRef":{"value":"58"},"TxnDate":"2026-10-01","DueDate":"2026-10-15","DocNumber":"ORDER-1042",
		"BillEmail":{"Address":"accounts@cityagency.example.com"},"CurrencyRef":{"value":"USD"},"CustomerMemo":{"value":"Thank you\nfor your order"},
		"PrivateNote":"Order ORDER-1042","Line":[
		{"DetailType":"SalesItemLineDetail","Amount":99.90,"Description":"Spring bouquet","SalesItemLineDetail":{"ItemRef":{"value":"1"},"Qty":2,"UnitPrice":49.95,"TaxCodeRef":{"value":"NON"},"ServiceDate":"2026-09-30"}},
		{"DetailType":"SalesItemLineDetail","Amount":1.00,"Description":"Card","SalesItemLineDetail":{"ItemRef":{"value":"2"},"Qty":3,"UnitPrice":0.33335}},
		{"DetailType":"SalesItemLineDetail","Amount":-1.50,"Description":"Discounted delivery","SalesItemLineDetail":{"ItemRef":{"value":"3"}}}]}`, request.body)
	require.Contains(t, request.body, `"Amount":99.90`, "amounts travel as exact JSON number literals")
	require.Contains(t, request.body, `"Amount":1.00`, "3 x 0.33335 = 1.00005 rounds half away from zero to 1.00")
}

func TestCreateInvoiceValidatesBeforeAnyRequestAndRejectionsAreConclusive(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusBadRequest, faultJSON("ValidationFault", "6140"))
		case 1:
			writeJSON(t, response, http.StatusBadRequest, faultJSON("ValidationFault", "2500"))
		default:
			invoice := invoiceJSON()
			invoice["CustomerRef"] = map[string]any{"value": "999"}
			writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoice})
		}
	})
	client := newQuickBooksClient(t, provider.URL)
	for name, change := range map[string]func(*quickbooks.CreateInvoiceInput){
		"customer":         func(input *quickbooks.CreateInvoiceInput) { input.CustomerID = "C-58" },
		"no lines":         func(input *quickbooks.CreateInvoiceInput) { input.Lines = nil },
		"no item":          func(input *quickbooks.CreateInvoiceInput) { input.Lines[0].ItemID = "" },
		"no amount":        func(input *quickbooks.CreateInvoiceInput) { input.Lines[0].UnitPrice = "" },
		"float price":      func(input *quickbooks.CreateInvoiceInput) { input.Lines[0].UnitPrice = "4.995e1" },
		"cents":            func(input *quickbooks.CreateInvoiceInput) { input.Lines[2].Amount = "-1.505" },
		"huge amount":      func(input *quickbooks.CreateInvoiceInput) { input.Lines[2].Amount = "12345678901" },
		"long number":      func(input *quickbooks.CreateInvoiceInput) { input.DocNumber = strings.Repeat("9", 22) },
		"backslash number": func(input *quickbooks.CreateInvoiceInput) { input.DocNumber = `A\1` },
		"due before":       func(input *quickbooks.CreateInvoiceInput) { input.DueDate = "2026-09-01" },
		"date format":      func(input *quickbooks.CreateInvoiceInput) { input.TxnDate = "10/01/2026" },
		"bill email":       func(input *quickbooks.CreateInvoiceInput) { input.BillEmail = "City <a@example.com>" },
		"tax code":         func(input *quickbooks.CreateInvoiceInput) { input.Lines[0].TaxCodeID = "TAX RATE" },
		"negative count":   func(input *quickbooks.CreateInvoiceInput) { input.Lines[0].Quantity = "-2" },
		"huge product": func(input *quickbooks.CreateInvoiceInput) {
			input.Lines[0].Quantity, input.Lines[0].UnitPrice = "999999999999", "999999999999"
		},
	} {
		input := validCreateInvoiceInput()
		input.Lines = append([]quickbooks.InvoiceLineInput(nil), input.Lines...)
		change(&input)
		result, err := sdkgo.RunMutation(newQuickBooksDexContext(name), client.CreateInvoice(), quickbooksConnection, input)
		require.NoError(t, err)
		require.Equal(t, quickbooks.CreateInvoiceBranchDefect, result.Branch, name)
		require.NotContains(t, result.Failure.Message, "4.995e1", "errors name the field, never the value")
	}
	require.Zero(t, provider.requestCount())

	result, err := sdkgo.RunMutation(newQuickBooksDexContext("duplicate"), client.CreateInvoice(), quickbooksConnection, validCreateInvoiceInput())
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateInvoiceBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind, "a duplicate document number is a conflict")
	require.Equal(t, "6140", result.Receipt.Metadata[quickbooks.FaultCodeReceiptKey])
	result, err = sdkgo.RunMutation(newQuickBooksDexContext("unknown-item"), client.CreateInvoice(), quickbooksConnection, validCreateInvoiceInput())
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateInvoiceBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	result, err = sdkgo.RunMutation(newQuickBooksDexContext("another-customer"), client.CreateInvoice(), quickbooksConnection, validCreateInvoiceInput())
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateInvoiceBranchUncertain, result.Branch, "an accepted answer about another customer is never trusted")
	require.Equal(t, 3, provider.requestCount())
}

func TestCreateInvoiceRetriesALostResponseAndADuplicateRequestIDUnderTheSameRequestID(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		switch index {
		case 0:
			dropConnection(t, response)
		case 1:
			writeJSON(t, response, http.StatusBadRequest, faultJSON("ValidationFault", "600"))
		default:
			writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoiceJSON()})
		}
	})
	client := newQuickBooksClient(t, provider.URL)
	for attempt := range 2 {
		_, err := sdkgo.RunMutation(newQuickBooksDexContext("retried"), client.CreateInvoice(), quickbooksConnection, validCreateInvoiceInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "attempt %d is retried", attempt)
	}
	result, err := sdkgo.RunMutation(newQuickBooksDexContext("retried"), client.CreateInvoice(), quickbooksConnection, validCreateInvoiceInput())
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateInvoiceBranchCreated, result.Branch)
	requestID := provider.request(0).query.Get("requestid")
	require.NotEmpty(t, requestID)
	for index := range 3 {
		require.Equal(t, requestID, provider.request(index).query.Get("requestid"), "one Step execution keeps one requestid")
		require.Equal(t, provider.request(0).body, provider.request(index).body, "the body is identical on every attempt")
	}
}

func TestGetInvoiceReadsExactDecimalsAndQuickBooksValues(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoiceJSON(), "time": "2026-10-01T02:06:00.000-07:00"})
	})
	result, err := sdkgo.RunQuery(newQuickBooksDexContext("get"), newQuickBooksClient(t, provider.URL).GetInvoice(), quickbooksConnection,
		quickbooks.GetInvoiceInput{InvoiceID: " " + testInvoiceID + " "})
	require.NoError(t, err)
	require.Equal(t, quickbooks.GetInvoiceBranchFound, result.Branch)
	invoice := result.Value
	require.Equal(t, "ORDER-1042", invoice.DocNumber)
	require.Equal(t, testCustomerID, invoice.CustomerID)
	require.Equal(t, quickbooks.Decimal("99.90"), invoice.TotalAmount, "QuickBooks's digits are kept")
	require.Equal(t, quickbooks.Decimal("0"), invoice.Balance)
	require.True(t, invoice.Balance.IsZero())
	require.Equal(t, quickbooks.EmailStatusEmailSent, invoice.EmailStatus)
	require.Equal(t, "Thank you", invoice.CustomerMemo)
	require.Equal(t, []string{testPaymentID}, invoice.LinkedPaymentIDs)
	require.Equal(t, time.Date(2026, 10, 1, 9, 5, 0, 0, time.UTC), *invoice.DeliveredAt)
	require.Len(t, invoice.Lines, 2)
	require.Equal(t, quickbooks.InvoiceLine{
		LineID: "1", DetailType: quickbooks.LineDetailTypeSalesItem, Description: "Spring bouquet", Amount: "99.90", ItemID: "1",
		ItemName: "Services", Quantity: "2", UnitPrice: "49.95", TaxCodeID: "NON", ServiceDate: "2026-09-30",
	}, invoice.Lines[0])
	require.Equal(t, quickbooks.LineDetailTypeSubTotal, invoice.Lines[1].DetailType, "QuickBooks's own line types pass through")
	require.Equal(t, companyPath+"/invoice/"+testInvoiceID, provider.request(0).path)
	require.Empty(t, provider.request(0).query.Get("requestid"), "reads carry no requestid")
}

func TestGetInvoiceRejectsAnotherInvoiceExponentsAndBadInput(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		invoice := invoiceJSON()
		if index == 1 {
			invoice["TotalAmt"] = json.Number("9.99E1")
			invoice["Balance"] = json.Number("1E-2")
		}
		writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoice})
	})
	client := newQuickBooksClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newQuickBooksDexContext("another"), client.GetInvoice(), quickbooksConnection, quickbooks.GetInvoiceInput{InvoiceID: "131"})
	require.NoError(t, err)
	require.Equal(t, quickbooks.GetInvoiceBranchInvalidResponse, result.Branch, "an answer about another invoice is never accepted")
	result, err = sdkgo.RunQuery(newQuickBooksDexContext("exponent"), client.GetInvoice(), quickbooksConnection, quickbooks.GetInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, quickbooks.GetInvoiceBranchFound, result.Branch)
	require.Equal(t, quickbooks.Decimal("99.9"), result.Value.TotalAmount, "an exponent is expanded exactly")
	require.Equal(t, quickbooks.Decimal("0.01"), result.Value.Balance)
	for _, invoiceID := range []string{"", "INV-1", "1/2"} {
		result, err = sdkgo.RunQuery(newQuickBooksDexContext("bad-"+invoiceID), client.GetInvoice(), quickbooksConnection, quickbooks.GetInvoiceInput{InvoiceID: invoiceID})
		require.NoError(t, err)
		require.Equal(t, quickbooks.GetInvoiceBranchDefect, result.Branch, invoiceID)
	}
	require.Equal(t, 2, provider.requestCount())
}
