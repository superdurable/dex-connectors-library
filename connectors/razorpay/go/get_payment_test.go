// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package razorpay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "razorpay", Name: "razorpay"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "razorpay-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-payment" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{KeyID: sdkgo.NewSecretString("rzp_test_1234567890"), KeySecret: sdkgo.NewSecretString("sample-razorpay-secret")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetPaymentReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/payments/pay_DG4ZdRK8ZnXC3k", request.URL.Path)
		require.Equal(t, "Basic cnpwX3Rlc3RfMTIzNDU2Nzg5MDpzYW1wbGUtcmF6b3JwYXktc2VjcmV0", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"id":"pay_DG4ZdRK8ZnXC3k","entity":"payment","status":"captured","amount":100,"currency":"INR","email":"private@example.com"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetPayment(), testConnection, GetPaymentInput{PaymentID: "pay_DG4ZdRK8ZnXC3k"})
	require.NoError(t, err)
	require.Equal(t, GetPaymentBranchFound, result.Branch)
	require.Equal(t, PaymentStatus{ID: "pay_DG4ZdRK8ZnXC3k", Status: "captured", Amount: 100, CurrencyCode: "INR"}, result.Value)
}

func TestGetPaymentClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetPaymentBranchNotFound},
		{name: "forbidden", status: 403, branch: GetPaymentBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"id":"other","entity":"payment","status":"captured","amount":100,"currency":"INR"}`, branch: GetPaymentBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetPayment(), testConnection, GetPaymentInput{PaymentID: "pay_DG4ZdRK8ZnXC3k"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestGetPaymentRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.razorpay.com/v1", 0)
	for _, paymentID := range []string{"", "../other", "bad\npayment", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetPayment(), testConnection, GetPaymentInput{PaymentID: paymentID})
		require.NoError(t, err)
		require.Equal(t, GetPaymentBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"id":"pay_DG4ZdRK8ZnXC3k","entity":"payment","status":"captured","amount":100,"currency":"INR"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 20).GetPayment(), testConnection, GetPaymentInput{PaymentID: "pay_DG4ZdRK8ZnXC3k"})
	require.NoError(t, err)
	require.Equal(t, GetPaymentBranchInvalidResponse, result.Branch)
}
