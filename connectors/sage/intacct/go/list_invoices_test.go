// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sageintacct

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListInvoicesReadsOnlyIdentities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/ia/api/v1/objects/accounts-receivable/invoice", request.URL.Path)
		require.Equal(t, "Bearer test-access-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"ia::result":[{"key":"443","id":"INV-443","customer":{"id":"private"}}],"ia::meta":{"next":""}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).ListInvoices(), testConnection, ListInvoicesInput{})
	require.NoError(t, err)
	require.Equal(t, ListInvoicesBranchFound, result.Branch)
	require.Equal(t, InvoiceList{Invoices: []InvoiceSummary{{Key: "443", ID: "INV-443"}}}, result.Value)
}

func TestListInvoicesClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"forbidden", 403, `{}`, ListInvoicesBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, ListInvoicesBranchInvalidResponse, false},
		{"missing result", 200, `{}`, ListInvoicesBranchInvalidResponse, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, testClient(t, server.URL).ListInvoices(), testConnection, ListInvoicesInput{})
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
