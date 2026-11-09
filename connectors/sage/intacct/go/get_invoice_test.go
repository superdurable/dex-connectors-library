// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sageintacct

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "sage-intacct", Name: "sage-intacct"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "sage-intacct-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-invoice" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func testClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	credentials := Credentials{
		OAuthClientID: "test-client", OAuthClientSecret: sdkgo.NewSecretString("test-secret"),
		AccessToken: sdkgo.NewSecretString("test-access-token"), RefreshToken: sdkgo.NewSecretString("test-refresh-token"),
	}
	client, err := New(Config{BaseURL: serverURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetInvoiceReadsOnlySelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/ia/api/v1/objects/accounts-receivable/invoice/443", request.URL.Path)
		require.Equal(t, "Bearer test-access-token", request.Header.Get("Authorization"))
		require.Empty(t, request.URL.Query().Get("access_token"))
		_, err := writer.Write([]byte(`{"ia::result":{"key":"443","id":"INV-443","customer":{"id":"private"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).GetInvoice(), testConnection, GetInvoiceInput{InvoiceKey: "443"})
	require.NoError(t, err)
	require.Equal(t, GetInvoiceBranchFound, result.Branch)
	require.Equal(t, InvoiceSummary{Key: "443", ID: "INV-443"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), "test-access-token")
}

func TestGetInvoiceClassifiesProviderResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 404, `{}`, GetInvoiceBranchNotFound, false},
		{"denied", 403, `{}`, GetInvoiceBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"timeout", 408, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"invalid JSON", 200, `{`, GetInvoiceBranchInvalidResponse, false},
		{"wrong invoice", 200, `{"ia::result":{"key":"444","id":"INV-444"}}`, GetInvoiceBranchInvalidResponse, false},
		{"missing invoice ID", 200, `{"ia::result":{"key":"443"}}`, GetInvoiceBranchInvalidResponse, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).GetInvoice(), testConnection, GetInvoiceInput{InvoiceKey: "443"})
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

func TestGetInvoiceRejectsUnsafeKeyBeforeProviderCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("invalid invoice key reached provider")
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).GetInvoice(), testConnection, GetInvoiceInput{InvoiceKey: "../other"})
	require.NoError(t, err)
	require.Equal(t, GetInvoiceBranchDefect, result.Branch)
}

func TestGetInvoiceRetriesTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client := testClient(t, server.URL)
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, client.GetInvoice(), testConnection, GetInvoiceInput{InvoiceKey: "443"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.False(t, errors.Is(err, context.Canceled))
}
