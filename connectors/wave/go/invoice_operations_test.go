// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wave

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestInvoiceOperationsUseWaveGraphQL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/graphql/public", request.URL.Path)
		require.Equal(t, "Bearer sample-wave-access-token", request.Header.Get("Authorization"))
		var body struct {
			Query     string          `json:"query"`
			Variables json.RawMessage `json:"variables"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		invoice := `{"id":"invoice-1","status":"DRAFT","title":"Example Invoice","invoiceNumber":"1001","customer":{"id":"customer-1"}}`
		switch {
		case strings.Contains(body.Query, "query ListInvoices"):
			require.JSONEq(t, `{"businessId":"business-1","page":1,"pageSize":20}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"business":{"invoices":{"pageInfo":{"currentPage":1,"totalPages":1,"totalCount":1},"edges":[{"node":` + invoice + `}]}}}}`))
			require.NoError(t, err)
		case strings.Contains(body.Query, "query GetInvoice"):
			require.JSONEq(t, `{"businessId":"business-1","invoiceId":"invoice-1"}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"business":{"invoice":` + invoice + `}}}`))
			require.NoError(t, err)
		case strings.Contains(body.Query, "mutation CreateInvoice"):
			require.JSONEq(t, `{"input":{"businessId":"business-1","customerId":"customer-1","items":[{"productId":"product-1"}]}}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"invoiceCreate":{"didSucceed":true,"invoice":` + invoice + `}}}`))
			require.NoError(t, err)
		case strings.Contains(body.Query, "mutation UpdateInvoice"):
			require.JSONEq(t, `{"input":{"id":"invoice-1","title":"Updated Invoice"}}`, string(body.Variables))
			updatedInvoice := strings.Replace(invoice, "Example Invoice", "Updated Invoice", 1)
			_, err := writer.Write([]byte(`{"data":{"invoicePatch":{"didSucceed":true,"invoice":` + updatedInvoice + `}}}`))
			require.NoError(t, err)
		case strings.Contains(body.Query, "mutation ApproveInvoice"):
			require.JSONEq(t, `{"input":{"invoiceId":"invoice-1"}}`, string(body.Variables))
			approvedInvoice := strings.Replace(invoice, "DRAFT", "SAVED", 1)
			_, err := writer.Write([]byte(`{"data":{"invoiceApprove":{"didSucceed":true,"invoice":` + approvedInvoice + `}}}`))
			require.NoError(t, err)
		case strings.Contains(body.Query, "mutation DeleteInvoice"):
			require.JSONEq(t, `{"input":{"invoiceId":"invoice-1"}}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"invoiceDelete":{"didSucceed":true}}}`))
			require.NoError(t, err)
		case strings.Contains(body.Query, "mutation SendInvoice"):
			require.JSONEq(t, `{"input":{"invoiceId":"invoice-1","to":["buyer@example.com"],"attachPDF":true}}`, string(body.Variables))
			_, err := writer.Write([]byte(`{"data":{"invoiceSend":{"didSucceed":true}}}`))
			require.NoError(t, err)
		default:
			t.Fatal("unexpected Wave invoice operation")
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	listed, err := sdkgo.RunQuery(queryContext{}, client.ListInvoices(), testConnection, ListInvoicesInput{BusinessID: "business-1", Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, listed.Value.Invoices, 1)
	found, err := sdkgo.RunQuery(queryContext{}, client.GetInvoice(), testConnection, GetInvoiceInput{BusinessID: "business-1", InvoiceID: "invoice-1"})
	require.NoError(t, err)
	require.Equal(t, "invoice-1", found.Value.ID)
	created, err := sdkgo.RunMutation(&customerTestContext{}, client.CreateInvoice(), testConnection, CreateInvoiceInput{BusinessID: "business-1", CustomerID: "customer-1", ProductID: "product-1"})
	require.NoError(t, err)
	require.Equal(t, CreateInvoiceBranchCreated, created.Branch)
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdateInvoice(), testConnection, UpdateInvoiceInput{BusinessID: "business-1", InvoiceID: "invoice-1", Title: "Updated Invoice"})
	require.NoError(t, err)
	require.Equal(t, "Updated Invoice", updated.Value.Title)
	approved, err := sdkgo.RunMutation(queryContext{}, client.ApproveInvoice(), testConnection, ApproveInvoiceInput{BusinessID: "business-1", InvoiceID: "invoice-1"})
	require.NoError(t, err)
	require.Equal(t, "SAVED", approved.Value.Status)
	deleted, err := sdkgo.RunMutation(queryContext{}, client.DeleteInvoice(), testConnection, DeleteInvoiceInput{InvoiceID: "invoice-1"})
	require.NoError(t, err)
	require.Equal(t, DeleteInvoiceBranchDeleted, deleted.Branch)
	sent, err := sdkgo.RunMutation(&customerTestContext{}, client.SendInvoice(), testConnection, SendInvoiceInput{InvoiceID: "invoice-1", To: []string{"buyer@example.com"}, AttachPDF: true})
	require.NoError(t, err)
	require.Equal(t, SendInvoiceBranchSent, sent.Branch)
}
