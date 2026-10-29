// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package square

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "square", Name: "square"}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: Credentials{AccessToken: sdkgo.NewSecretString("test-token")}})
	require.NoError(t, err)
	return client
}

func TestListPaymentsReadsAndClassifiesProviderStatus(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.RawQuery, "test-token")
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v2/payments", r.URL.Path)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, err := fmt.Fprint(w, `{"error":"SENTINEL"}`)
			require.NoError(t, err)
			return
		}
		_, err := fmt.Fprint(w, "{\"payments\":[{\"id\":\"pay_101\",\"status\":\"COMPLETED\",\"amount_money\":{\"amount\":2900,\"currency\":\"USD\"}}],\"cursor\":\"\"}")
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v2")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListPayments(), testConnection, ListPaymentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListPaymentsBranchListed, result.Branch)
	require.Nil(t, result.Failure)
	require.Len(t, result.Value.Payments, 1)
	status = http.StatusNotFound
	rejected, err := sdkgo.RunQuery(queryContext{}, client.ListPayments(), testConnection, ListPaymentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListPaymentsBranchNotFound, rejected.Branch)
	require.NotContains(t, rejected.Failure.Message, "SENTINEL")
}

func TestGetPaymentReadsAndClassifiesProviderStatus(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.RawQuery, "test-token")
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v2/payments/pay_101", r.URL.Path)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, err := fmt.Fprint(w, `{"error":"SENTINEL"}`)
			require.NoError(t, err)
			return
		}
		_, err := fmt.Fprint(w, "{\"payment\":{\"id\":\"pay_101\",\"status\":\"COMPLETED\",\"amount_money\":{\"amount\":2900,\"currency\":\"USD\"}}}")
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v2")
	result, err := sdkgo.RunQuery(queryContext{}, client.GetPayment(), testConnection, GetPaymentInput{PaymentID: "pay_101"})
	require.NoError(t, err)
	require.Equal(t, GetPaymentBranchFound, result.Branch)
	require.Nil(t, result.Failure)
	require.NotEmpty(t, result.Value.ID)
	status = http.StatusNotFound
	rejected, err := sdkgo.RunQuery(queryContext{}, client.GetPayment(), testConnection, GetPaymentInput{PaymentID: "pay_101"})
	require.NoError(t, err)
	require.Equal(t, GetPaymentBranchNotFound, rejected.Branch)
	require.NotContains(t, rejected.Failure.Message, "SENTINEL")
}
