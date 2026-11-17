// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickhouse

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListDatabasesReadsBoundedCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, listDatabasesSQL, request.URL.Query().Get("query"))
		_, err := writer.Write([]byte(`{"data":[{"name":"analytics"},{"name":"default"}],"rows":2}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListDatabases(), testConnection, ListDatabasesInput{})
	require.NoError(t, err)
	require.Equal(t, ListDatabasesBranchListed, result.Branch)
	require.Equal(t, DatabasesPage{Names: []string{"analytics", "default"}}, result.Value)
}

func TestListColumnsReadsSelectedTable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, listColumnsSQL, request.URL.Query().Get("query"))
		require.Equal(t, "analytics", request.URL.Query().Get("param_database"))
		require.Equal(t, "events", request.URL.Query().Get("param_table"))
		_, err := writer.Write([]byte(`{"data":[{"name":"id","type":"UInt64"},{"name":"event","type":"String"}],"rows":2}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListColumns(), testConnection, ListColumnsInput{Database: "analytics", Table: "events"})
	require.NoError(t, err)
	require.Equal(t, ListColumnsBranchListed, result.Branch)
	require.Equal(t, ColumnsPage{Database: "analytics", Table: "events", Columns: []Column{{Name: "id", Type: "UInt64"}, {Name: "event", Type: "String"}}}, result.Value)
}

func TestMetadataQueriesClassifyProviderResponses(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "forbidden", status: 403, branch: ListColumnsBranchProviderRejected},
		{name: "invalid JSON", status: 200, body: `{"data":[{"name":"id","type":""}],"rows":1}`, branch: ListColumnsBranchInvalidResponse},
		{name: "rate limited", status: 429, retry: true},
		{name: "server error", status: 500, retry: true},
		{name: "unavailable", status: 503, retry: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListColumns(), testConnection, ListColumnsInput{Database: "analytics", Table: "events"})
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

func TestListColumnsRejectsInvalidInput(t *testing.T) {
	client := newTestClient(t, "https://example.clickhouse.cloud:8443", 0)
	for _, input := range []ListColumnsInput{{Database: "", Table: "events"}, {Database: "analytics", Table: " bad "}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListColumns(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListColumnsBranchDefect, result.Branch)
	}
}
