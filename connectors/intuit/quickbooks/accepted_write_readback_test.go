// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
	"context"
	"net/http"
	"sync/atomic"
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
			writeValue(t, response, http.StatusOK, map[string]any{"Payment": paymentJSON(invoiceID, "99.9")})
		})
		result, err := sdkgo.RunMutation(newQuickBooksDexContext("payment-readback"), newQuickBooksClient(t, provider.URL).RecordPayment(), quickbooksConnection,
			validRecordPaymentInput())
		require.NoError(t, err)
		require.Equal(t, quickbooks.RecordPaymentBranchRecorded, result.Branch)
		require.Equal(t, testPaymentID, result.Value.PaymentID)
		require.Equal(t, quickbooks.Decimal("99.9"), result.Value.TotalAmount)
		require.Equal(t, companyPath+"/payment/186", provider.request(1).path)
		assertOneAcceptedWriteAndOneRead(t, provider)
	})
	t.Run("send", func(t *testing.T) {
		provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
			if index == 0 {
				writeJSON(t, response, http.StatusOK, `{"Invoice":{}}`)
				return
			}
			invoice := invoiceJSON()
			invoice["BillEmail"] = map[string]any{"Address": "ACCOUNTS@CITYAGENCY.EXAMPLE.COM"}
			writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoice})
		})
		result, err := sdkgo.RunMutation(newQuickBooksDexContext("send-readback"), newQuickBooksClient(t, provider.URL).SendInvoice(), quickbooksConnection,
			quickbooks.SendInvoiceInput{InvoiceID: testInvoiceID, SendTo: "accounts@cityagency.example.com"})
		require.NoError(t, err)
		require.Equal(t, quickbooks.SendInvoiceBranchSent, result.Branch)
		require.Equal(t, companyPath+"/invoice/130", provider.request(1).path)
		assertOneAcceptedWriteAndOneRead(t, provider)
	})
}

func TestInterruptedAcceptedWriteNeverResends(t *testing.T) {
	tests := []struct {
		name           string
		expectedBranch sdkgo.BranchID
		expectedReads  int
		invoke         func(*testing.T, *recordingQuickBooks) sdkgo.BranchID
	}{
		{"customer", quickbooks.CreateCustomerBranchUncertain, 0, func(t *testing.T, provider *recordingQuickBooks) sdkgo.BranchID {
			result, err := sdkgo.RunMutation(newQuickBooksDexContext("interrupted-customer"), newQuickBooksClient(t, provider.URL).CreateCustomer(), quickbooksConnection,
				quickbooks.CreateCustomerInput{DisplayName: "City Agency", EmailAddress: "accounts@cityagency.example.com"})
			require.NoError(t, err)
			return result.Branch
		}},
		{"invoice", quickbooks.CreateInvoiceBranchUncertain, 0, func(t *testing.T, provider *recordingQuickBooks) sdkgo.BranchID {
			result, err := sdkgo.RunMutation(newQuickBooksDexContext("interrupted-invoice"), newQuickBooksClient(t, provider.URL).CreateInvoice(), quickbooksConnection, validCreateInvoiceInput())
			require.NoError(t, err)
			return result.Branch
		}},
		{"payment", quickbooks.RecordPaymentBranchUncertain, 0, func(t *testing.T, provider *recordingQuickBooks) sdkgo.BranchID {
			result, err := sdkgo.RunMutation(newQuickBooksDexContext("interrupted-payment"), newQuickBooksClient(t, provider.URL).RecordPayment(), quickbooksConnection, validRecordPaymentInput())
			require.NoError(t, err)
			return result.Branch
		}},
		{"send", quickbooks.SendInvoiceBranchSent, 1, func(t *testing.T, provider *recordingQuickBooks) sdkgo.BranchID {
			result, err := sdkgo.RunMutation(newQuickBooksDexContext("interrupted-send"), newQuickBooksClient(t, provider.URL).SendInvoice(), quickbooksConnection,
				quickbooks.SendInvoiceInput{InvoiceID: testInvoiceID, SendTo: "accounts@cityagency.example.com"})
			require.NoError(t, err)
			return result.Branch
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, request recordedRequest, index int) {
				if index == 0 {
					require.Equal(t, http.MethodPost, request.method)
					response.Header().Set("Content-Length", "500")
					response.WriteHeader(http.StatusOK)
					_, err := response.Write([]byte(`{"Invoice":`))
					require.NoError(t, err)
					return
				}
				writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoiceJSON()})
			})
			require.Equal(t, test.expectedBranch, test.invoke(t, provider))
			require.Equal(t, 1+test.expectedReads, provider.requestCount())
			for index := 1; index < provider.requestCount(); index++ {
				require.Equal(t, http.MethodGet, provider.request(index).method)
			}
		})
	}
}

func TestAcceptedCreateOrPaymentWithUnavailableReadBackRemainsUncertain(t *testing.T) {
	tests := []struct {
		name   string
		entity string
		invoke func(*testing.T, *recordingQuickBooks) sdkgo.BranchID
	}{
		{"customer", "Customer", func(t *testing.T, provider *recordingQuickBooks) sdkgo.BranchID {
			result, err := sdkgo.RunMutation(newQuickBooksDexContext("customer-readback-503"), newQuickBooksClient(t, provider.URL).CreateCustomer(), quickbooksConnection,
				quickbooks.CreateCustomerInput{DisplayName: "City Agency"})
			require.NoError(t, err)
			return result.Branch
		}},
		{"invoice", "Invoice", func(t *testing.T, provider *recordingQuickBooks) sdkgo.BranchID {
			result, err := sdkgo.RunMutation(newQuickBooksDexContext("invoice-readback-503"), newQuickBooksClient(t, provider.URL).CreateInvoice(), quickbooksConnection, validCreateInvoiceInput())
			require.NoError(t, err)
			return result.Branch
		}},
		{"payment", "Payment", func(t *testing.T, provider *recordingQuickBooks) sdkgo.BranchID {
			result, err := sdkgo.RunMutation(newQuickBooksDexContext("payment-readback-503"), newQuickBooksClient(t, provider.URL).RecordPayment(), quickbooksConnection, validRecordPaymentInput())
			require.NoError(t, err)
			return result.Branch
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
				if index == 0 {
					writeJSON(t, response, http.StatusOK, `{"`+test.entity+`":{"Id":"130"}}`)
					return
				}
				writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
			})
			require.Equal(t, sdkgo.BranchID("uncertain"), test.invoke(t, provider))
			assertOneAcceptedWriteAndOneRead(t, provider)
		})
	}
}

func TestAcceptedCreateWithoutIDRemainsUncertainAfterOneRequest(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusOK, `{"Customer":{}}`)
	})
	result, err := sdkgo.RunMutation(newQuickBooksDexContext("customer-no-id"), newQuickBooksClient(t, provider.URL).CreateCustomer(), quickbooksConnection,
		quickbooks.CreateCustomerInput{DisplayName: "City Agency"})
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateCustomerBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
	require.Equal(t, http.MethodPost, provider.request(0).method)
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

func TestMutationWithLessThanTenthOfAttemptBudgetDoesNotSend(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request is expected") })
	startedAt := time.Now()
	requestContext, cancel := context.WithDeadline(context.Background(), startedAt.Add(30*time.Second))
	defer cancel()
	var clockCalls atomic.Int32
	clock := func() time.Time {
		if clockCalls.Add(1) == 1 {
			return startedAt
		}
		return startedAt.Add(28 * time.Second)
	}
	dexContext := newQuickBooksDexContext("short-write-window")
	dexContext.Context = requestContext
	_, err := sdkgo.RunMutation(dexContext, newQuickBooksClient(t, provider.URL, quickbooks.WithClock(clock)).RecordPayment(), quickbooksConnection, validRecordPaymentInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Contains(t, retry.Failure.Message, "not sent")
	require.Zero(t, provider.requestCount())
}

func TestReadWithTwoSecondsOfAttemptTimeStillRuns(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		require.Equal(t, http.MethodGet, request.method)
		writeValue(t, response, http.StatusOK, map[string]any{"Invoice": invoiceJSON()})
	})
	startedAt := time.Now()
	requestContext, cancel := context.WithDeadline(context.Background(), startedAt.Add(30*time.Second))
	defer cancel()
	dexContext := newQuickBooksDexContext("short-read-window")
	dexContext.Context = requestContext
	client := newQuickBooksClient(t, provider.URL, quickbooks.WithClock(func() time.Time { return startedAt.Add(28 * time.Second) }))
	result, err := sdkgo.RunQuery(dexContext, client.GetInvoice(), quickbooksConnection, quickbooks.GetInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, quickbooks.GetInvoiceBranchFound, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestDeadlineCutsAnAcceptedWriteMidResponseWithoutResending(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		response.Header().Set("Content-Length", "500")
		response.WriteHeader(http.StatusOK)
		_, err := response.Write([]byte(`{"Customer":`))
		require.NoError(t, err)
		response.(http.Flusher).Flush()
		<-time.After(4500 * time.Millisecond)
	})
	dexContext := newQuickBooksDexContext("deadline-mid-response")
	requestContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(4*time.Second))
	defer cancel()
	dexContext.Context = requestContext
	result, err := sdkgo.RunMutation(dexContext, newQuickBooksClient(t, provider.URL).CreateCustomer(), quickbooksConnection,
		quickbooks.CreateCustomerInput{DisplayName: "City Agency"})
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateCustomerBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
	require.Equal(t, http.MethodPost, provider.request(0).method)
}

func TestInterruptedReadRetries(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		response.Header().Set("Content-Length", "500")
		response.WriteHeader(http.StatusOK)
		_, err := response.Write([]byte(`{"Invoice":`))
		require.NoError(t, err)
	})
	_, err := sdkgo.RunQuery(newQuickBooksDexContext("interrupted-read"), newQuickBooksClient(t, provider.URL).GetInvoice(), quickbooksConnection,
		quickbooks.GetInvoiceInput{InvoiceID: testInvoiceID})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 1, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(0).method)
}

func assertOneAcceptedWriteAndOneRead(t *testing.T, provider *recordingQuickBooks) {
	t.Helper()
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodPost, provider.request(0).method)
	require.Equal(t, http.MethodGet, provider.request(1).method)
	require.NotEmpty(t, provider.request(0).query.Get("requestid"))
	require.Empty(t, provider.request(1).query.Get("requestid"))
}
