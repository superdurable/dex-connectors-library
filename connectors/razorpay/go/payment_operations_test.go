// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package razorpay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const testPaymentID = "pay_DG4ZdRK8ZnXC3k"

func TestListPaymentsExcludesCustomerDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/api/payments", request.URL.Path)
		require.Equal(t, "2", request.URL.Query().Get("count"))
		require.Equal(t, "1", request.URL.Query().Get("skip"))
		_, err := writer.Write([]byte(`{"entity":"collection","count":1,"items":[{"id":"` + testPaymentID + `","entity":"payment","amount":100,"currency":"INR","status":"captured","email":"private@example.com"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).ListPayments(), testConnection, ListPaymentsInput{Count: 2, Skip: 1})
	require.NoError(t, err)
	require.Equal(t, ListPaymentsBranchListed, result.Branch)
	require.Len(t, result.Value.Payments, 1)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private@example.com")
}

func TestCapturePaymentConfirmsStateAfterDuplicateRejection(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method == http.MethodPost {
			require.Equal(t, "/api/payments/"+testPaymentID+"/capture", request.URL.Path)
			var payload struct {
				Amount   int64  `json:"amount"`
				Currency string `json:"currency"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
			require.Equal(t, int64(100), payload.Amount)
			require.Equal(t, "INR", payload.Currency)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		require.Equal(t, "/api/payments/"+testPaymentID, request.URL.Path)
		_, err := writer.Write([]byte(`{"id":"` + testPaymentID + `","entity":"payment","amount":100,"currency":"INR","status":"captured"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	input := CapturePaymentInput{PaymentID: testPaymentID, Amount: 100, CurrencyCode: "INR"}
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("razorpay-capture", "capture-payment"), newTestClient(t, server.URL+"/api", 0).CapturePayment(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CapturePaymentBranchCaptured, result.Branch)
	require.Equal(t, 2, requests)
}
