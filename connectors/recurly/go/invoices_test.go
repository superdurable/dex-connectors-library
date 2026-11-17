// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recurly

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const testInvoiceID = "e28zov4fw0v2"

func TestInvoiceQueriesExcludeAccountAndPaymentDetails(t *testing.T) {
	summary := `{"id":"` + testInvoiceID + `","state":"open","account":{"email":"private@example.com"},"billing_info":{"number":"private"}}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/invoices" {
			require.Equal(t, "2", request.URL.Query().Get("limit"))
			_, err := writer.Write([]byte(`{"object":"list","has_more":false,"data":[` + summary + `]}`))
			require.NoError(t, err)
			return
		}
		require.Equal(t, "/api/invoices/"+testInvoiceID, request.URL.Path)
		_, err := writer.Write([]byte(summary))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api", 0)
	listed, err := sdkgo.RunQuery(queryContext{}, client.ListInvoices(), testConnection, ListInvoicesInput{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, ListInvoicesBranchListed, listed.Branch)
	found, err := sdkgo.RunQuery(queryContext{}, client.GetInvoice(), testConnection, GetInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, GetInvoiceBranchFound, found.Branch)
	encoded, err := json.Marshal(found.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
}
