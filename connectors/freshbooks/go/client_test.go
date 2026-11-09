// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshbooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessToken = "synthetic-freshbooks-token"

var testConnection = sdkgo.ConnectionRef{Provider: "freshbooks", Name: "freshbooks"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "freshbooks-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	credentials := Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}
	client, err := New(Config{BaseURL: baseURL, RedirectURI: "https://dex.example/callback"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetInvoiceProjectsProviderInvoiceAndCredentialsStaySecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/accounting/account/KAG77/invoices/invoices/2201278", r.URL.Path)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		_, err := w.Write([]byte(`{"response":{"result":{"invoice":{"accountid":"KAG77","invoiceid":2201278,"invoice_number":"0000003","amount":{"amount":"800.00","code":"USD"},"payment_status":"unpaid","notes":"not-persisted"}}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetInvoice(), testConnection, GetInvoiceInput{AccountID: "KAG77", InvoiceID: 2201278})
	require.NoError(t, err)
	require.Equal(t, GetInvoiceBranchFound, result.Branch)
	require.Equal(t, Invoice{AccountID: "KAG77", InvoiceID: 2201278, InvoiceNumber: "0000003", Amount: "800.00", CurrencyCode: "USD", PaymentStatus: "unpaid"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestGetInvoiceRejectsInvalidIDBeforeProviderCall(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetInvoice(), testConnection, GetInvoiceInput{AccountID: "invalid/id", InvoiceID: 2201278})
	require.NoError(t, err)
	require.Equal(t, GetInvoiceBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
