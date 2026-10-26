// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestAcceptedWritesReadBackByIDWithoutResending(t *testing.T) {
	t.Run("customer", func(t *testing.T) {
		provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
			if index == 0 {
				writeJSON(t, response, http.StatusOK, `{"Customer":{"Id":"58"}}`)
				return
			}
			writeValue(t, response, http.StatusOK, map[string]any{"Customer": map[string]any{"Id": testCustomerID, "DisplayName": "City Agency", "PrimaryEmailAddr": map[string]any{"Address": "accounts@cityagency.example.com"}}})
		})
		result, err := sdkgo.RunMutation(newQuickBooksDexContext("customer-readback"), newQuickBooksClient(t, provider.URL).CreateCustomer(), quickbooksConnection,
			quickbooks.CreateCustomerInput{DisplayName: "City Agency", EmailAddress: "accounts@cityagency.example.com"})
		require.NoError(t, err)
		require.Equal(t, quickbooks.CreateCustomerBranchCreated, result.Branch)
		require.Equal(t, testCustomerID, result.Value.Customer.CustomerID)
		require.Equal(t, companyPath+"/customer/58", provider.request(1).path)
		assertOneAcceptedWriteAndOneRead(t, provider)
	})
	t.Run("invoice", func(t *testing.T) {
		provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
			invoice := invoiceJSON()
			if index == 0 {
				invoice["CustomerRef"] = map[string]any{"value": "999"}
			}
			writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoice})
		})
		result, err := sdkgo.RunMutation(newQuickBooksDexContext("invoice-readback"), newQuickBooksClient(t, provider.URL).CreateInvoice(), quickbooksConnection,
			validCreateInvoiceInput())
		require.NoError(t, err)
		require.Equal(t, quickbooks.CreateInvoiceBranchCreated, result.Branch)
		require.Equal(t, testInvoiceID, result.Value.Invoice.InvoiceID)
		require.Equal(t, companyPath+"/invoice/130", provider.request(1).path)
		assertOneAcceptedWriteAndOneRead(t, provider)
	})
	t.Run("payment", func(t *testing.T) {
		provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
			invoiceID := testInvoiceID
			if index == 0 {
				invoiceID = "131"
			}
			writeValue(t, response, http.StatusOK, map[string]any{"Payment": paymentJSON(invoiceID, "99.90")})
		})
		result, err := sdkgo.RunMutation(newQuickBooksDexContext("payment-readback"), newQuickBooksClient(t, provider.URL).RecordPayment(), quickbooksConnection,
			validRecordPaymentInput())
		require.NoError(t, err)
		require.Equal(t, quickbooks.RecordPaymentBranchRecorded, result.Branch)
		require.Equal(t, testPaymentID, result.Value.PaymentID)
		require.Equal(t, companyPath+"/payment/186", provider.request(1).path)
		assertOneAcceptedWriteAndOneRead(t, provider)
	})
	t.Run("send", func(t *testing.T) {
		provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
			if index == 0 {
				writeJSON(t, response, http.StatusOK, `{"Invoice":{}}`)
				return
			}
			writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoiceJSON()})
		})
		result, err := sdkgo.RunMutation(newQuickBooksDexContext("send-readback"), newQuickBooksClient(t, provider.URL).SendInvoice(), quickbooksConnection,
			quickbooks.SendInvoiceInput{InvoiceID: testInvoiceID, SendTo: "accounts@cityagency.example.com"})
		require.NoError(t, err)
		require.Equal(t, quickbooks.SendInvoiceBranchSent, result.Branch)
		require.Equal(t, companyPath+"/invoice/130", provider.request(1).path)
		assertOneAcceptedWriteAndOneRead(t, provider)
	})
}

func TestAcceptedSendWithUnavailableReadBackRemainsUncertain(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, `{"Invoice":{}}`)
			return
		}
		writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
	})
	result, err := sdkgo.RunMutation(newQuickBooksDexContext("send-no-resend"), newQuickBooksClient(t, provider.URL).SendInvoice(), quickbooksConnection,
		quickbooks.SendInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, quickbooks.SendInvoiceBranchUncertain, result.Branch)
	assertOneAcceptedWriteAndOneRead(t, provider)
}

func TestWriteConflictAndUnavailableRetryUnderTheSameRequestID(t *testing.T) {
	for _, status := range []int{http.StatusConflict, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				writeJSON(t, response, status, `{}`)
			})
			_, err := sdkgo.RunMutation(newQuickBooksDexContext("retry-"+http.StatusText(status)), newQuickBooksClient(t, provider.URL).CreateInvoice(), quickbooksConnection,
				validCreateInvoiceInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, 1, provider.requestCount())
			require.NotEmpty(t, provider.request(0).query.Get("requestid"))
		})
	}
}

func TestDeadlineMarginPreventsAnAttemptFromStartingANewRequest(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request is expected") })
	dexContext := newQuickBooksDexContext("deadline")
	requestContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(100*time.Millisecond))
	defer cancel()
	dexContext.Context = requestContext
	_, err := sdkgo.RunMutation(dexContext, newQuickBooksClient(t, provider.URL).CreateInvoice(), quickbooksConnection, validCreateInvoiceInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Zero(t, provider.requestCount())
}

func assertOneAcceptedWriteAndOneRead(t *testing.T, provider *recordingQuickBooks) {
	t.Helper()
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodPost, provider.request(0).method)
	require.Equal(t, http.MethodGet, provider.request(1).method)
	require.NotEmpty(t, provider.request(0).query.Get("requestid"))
	require.Empty(t, provider.request(1).query.Get("requestid"))
}
