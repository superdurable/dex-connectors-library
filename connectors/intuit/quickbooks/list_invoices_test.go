// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListInvoicesBuildsOneQueryWithEveryFilter(t *testing.T) {
	updatedSince := time.Date(2026, 9, 30, 18, 30, 0, 0, time.FixedZone("PDT", -7*60*60))
	query, err := quickbooks.BuildListInvoicesQuery(quickbooks.ListInvoicesInput{
		CustomerID: testCustomerID, DocNumber: "O'NEIL-7", IsOpenOnly: true, TxnDateFrom: "2026-09-01", TxnDateTo: "2026-09-30",
		UpdatedSince: &updatedSince, StartPosition: 26, MaxResults: 25,
	})
	require.NoError(t, err)
	require.Equal(t, `select * from Invoice where CustomerRef = '58' AND DocNumber = 'O\'NEIL-7' AND Balance > '0' AND TxnDate >= '2026-09-01' AND `+
		`TxnDate <= '2026-09-30' AND MetaData.LastUpdatedTime >= '2026-10-01T01:30:00Z' ORDERBY MetaData.CreateTime STARTPOSITION 26 MAXRESULTS 25`, query)

	query, err = quickbooks.BuildListInvoicesQuery(quickbooks.ListInvoicesInput{})
	require.NoError(t, err)
	require.Equal(t, "select * from Invoice ORDERBY MetaData.CreateTime STARTPOSITION 1 MAXRESULTS 25", query)

	for name, input := range map[string]quickbooks.ListInvoicesInput{
		"negative start": {StartPosition: -1}, "page too large": {MaxResults: 101}, "customer": {CustomerID: "58 OR 1=1"},
		"backslash": {DocNumber: `A\`}, "date": {TxnDateFrom: "yesterday"},
	} {
		_, err := quickbooks.BuildListInvoicesQuery(input)
		require.Error(t, err, name)
	}
}

func TestListInvoicesPagesWithTheNextStartPosition(t *testing.T) {
	var pageSize int
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		invoices := make([]any, 0, pageSize)
		for index := range pageSize {
			invoice := invoiceJSON()
			invoice["Id"] = strconv.Itoa(200 + index)
			invoices = append(invoices, invoice)
		}
		queryResponse := map[string]any{}
		if pageSize > 0 {
			queryResponse = map[string]any{"Invoice": invoices, "startPosition": 1, "maxResults": pageSize}
		}
		writeValue(t, response, http.StatusOK, map[string]any{"QueryResponse": queryResponse})
	})
	client := newQuickBooksClient(t, provider.URL)

	pageSize = 2
	result, err := sdkgo.RunQuery(newQuickBooksDexContext("full"), client.ListInvoices(), quickbooksConnection, quickbooks.ListInvoicesInput{MaxResults: 2})
	require.NoError(t, err)
	require.Equal(t, quickbooks.ListInvoicesBranchListed, result.Branch)
	require.Len(t, result.Value.Invoices, 2)
	require.True(t, result.Value.HasMorePages, "a full page can be followed by more")
	require.Equal(t, 3, result.Value.NextStartPosition)

	pageSize = 0
	result, err = sdkgo.RunQuery(newQuickBooksDexContext("empty"), client.ListInvoices(), quickbooksConnection,
		quickbooks.ListInvoicesInput{StartPosition: 3, MaxResults: 2})
	require.NoError(t, err)
	require.Equal(t, quickbooks.ListInvoicesBranchListed, result.Branch)
	require.Empty(t, result.Value.Invoices)
	require.NotNil(t, result.Value.Invoices)
	require.False(t, result.Value.HasMorePages)
	require.Zero(t, result.Value.NextStartPosition)

	pageSize = 3
	result, err = sdkgo.RunQuery(newQuickBooksDexContext("overfull"), client.ListInvoices(), quickbooksConnection, quickbooks.ListInvoicesInput{MaxResults: 2})
	require.NoError(t, err)
	require.Equal(t, quickbooks.ListInvoicesBranchInvalidResponse, result.Branch, "more invoices than requested is a protocol error")
}

func TestListInvoicesRejectsAQueryResponseWithoutItsEnvelope(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusOK, `{"Invoice":[]}`)
	})
	result, err := sdkgo.RunQuery(newQuickBooksDexContext("envelope"), newQuickBooksClient(t, provider.URL).ListInvoices(), quickbooksConnection,
		quickbooks.ListInvoicesInput{})
	require.NoError(t, err)
	require.Equal(t, quickbooks.ListInvoicesBranchInvalidResponse, result.Branch)
}

func TestSendInvoicePostsAnEmptyOctetStreamAndRequiresEmailSent(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		invoice := invoiceJSON()
		if index == 1 {
			invoice["EmailStatus"] = "NeedToSend"
		}
		writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoice})
	})
	client := newQuickBooksClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newQuickBooksDexContext("send"), client.SendInvoice(), quickbooksConnection,
		quickbooks.SendInvoiceInput{InvoiceID: testInvoiceID, SendTo: "accounts@cityagency.example.com"})
	require.NoError(t, err)
	require.Equal(t, quickbooks.SendInvoiceBranchSent, result.Branch)
	require.Equal(t, quickbooks.EmailStatusEmailSent, result.Value.EmailStatus)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, companyPath+"/invoice/"+testInvoiceID+"/send", request.path)
	require.Equal(t, "application/octet-stream", request.header.Get("Content-Type"))
	require.Empty(t, request.body)
	require.Equal(t, "accounts@cityagency.example.com", request.query.Get("sendTo"))
	require.Equal(t, string(result.Receipt.IdempotencyKey), request.query.Get("requestid"))

	result, err = sdkgo.RunMutation(newQuickBooksDexContext("not-sent"), client.SendInvoice(), quickbooksConnection, quickbooks.SendInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, quickbooks.SendInvoiceBranchUncertain, result.Branch, "an accepted send that is not marked EmailSent is unproven")
	require.Empty(t, provider.request(1).query.Get("sendTo"), "blank sendTo uses the invoice's BillEmail")

	for name, input := range map[string]quickbooks.SendInvoiceInput{"no invoice": {}, "bad address": {InvoiceID: testInvoiceID, SendTo: "a@b@c"}} {
		result, err := sdkgo.RunMutation(newQuickBooksDexContext(name), client.SendInvoice(), quickbooksConnection, input)
		require.NoError(t, err)
		require.Equal(t, quickbooks.SendInvoiceBranchDefect, result.Branch, name)
	}
	require.Equal(t, 2, provider.requestCount())
}

func TestSendInvoiceOfAMissingInvoiceIsRejectedAsNotFound(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusBadRequest, faultJSON("ValidationFault", "610"))
	})
	result, err := sdkgo.RunMutation(newQuickBooksDexContext("missing"), newQuickBooksClient(t, provider.URL).SendInvoice(), quickbooksConnection,
		quickbooks.SendInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, quickbooks.SendInvoiceBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
}
