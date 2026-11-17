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

const testRefundID = "rfnd_FP8QHiV938haTz"

func TestCreateNormalRefundUsesStableProviderIdempotency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/payments/"+testPaymentID+"/refund", request.URL.Path)
		require.Regexp(t, `^[0-9a-f-]{36}$`, request.Header.Get("X-Refund-Idempotency"))
		var body struct {
			Amount int64 `json:"amount"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, int64(100), body.Amount)
		_, err := writer.Write([]byte(`{"id":"` + testRefundID + `","entity":"refund","payment_id":"` + testPaymentID + `","amount":100,"currency":"INR","status":"processed","notes":{"private":"ignored"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	input := CreateNormalRefundInput{PaymentID: testPaymentID, Amount: 100}
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("razorpay-refund", "create-normal-refund"), newTestClient(t, server.URL+"/api", 0).CreateNormalRefund(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateNormalRefundBranchCreated, result.Branch)
	require.Equal(t, testRefundID, result.Value.ID)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
}

func TestRefundQueriesExcludeBankAndCustomerFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		refund := `{"id":"` + testRefundID + `","entity":"refund","payment_id":"` + testPaymentID + `","amount":100,"currency":"INR","status":"processed","acquirer_data":{"arn":"private"}}`
		if request.URL.Path == "/api/refunds" {
			require.Equal(t, "2", request.URL.Query().Get("count"))
			require.Equal(t, "1", request.URL.Query().Get("skip"))
			_, err := writer.Write([]byte(`{"entity":"collection","count":1,"items":[` + refund + `]}`))
			require.NoError(t, err)
			return
		}
		require.Equal(t, "/api/refunds/"+testRefundID, request.URL.Path)
		_, err := writer.Write([]byte(refund))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api", 0)
	listed, err := sdkgo.RunQuery(queryContext{}, client.ListRefunds(), testConnection, ListRefundsInput{Count: 2, Skip: 1})
	require.NoError(t, err)
	require.Equal(t, ListRefundsBranchListed, listed.Branch)
	require.Len(t, listed.Value.Refunds, 1)
	found, err := sdkgo.RunQuery(queryContext{}, client.GetRefund(), testConnection, GetRefundInput{RefundID: testRefundID})
	require.NoError(t, err)
	require.Equal(t, GetRefundBranchFound, found.Branch)
	encoded, err := json.Marshal(found.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
}
