// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickhouse

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

var testConnection = sdkgo.ConnectionRef{Provider: "clickhouse", Name: "clickhouse"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "clickhouse-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-tables" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{Username: "reader", Password: sdkgo.NewSecretString("sample-clickhouse-password")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestListTablesReadsSelectedNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/", request.URL.Path)
		require.Equal(t, listTablesSQL, request.URL.Query().Get("query"))
		require.Equal(t, "analytics", request.URL.Query().Get("param_database"))
		require.Equal(t, "reader", request.Header.Get("X-ClickHouse-User"))
		require.Equal(t, "sample-clickhouse-password", request.Header.Get("X-ClickHouse-Key"))
		_, err := writer.Write([]byte(`{"data":[{"name":"events"},{"name":"users"}],"rows":2,"statistics":{"elapsed":0.001}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListTables(), testConnection, ListTablesInput{Database: "analytics"})
	require.NoError(t, err)
	require.Equal(t, ListTablesBranchListed, result.Branch)
	require.Equal(t, TablesPage{Database: "analytics", Names: []string{"events", "users"}}, result.Value)
}

func TestListTablesClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "forbidden", status: 403, branch: ListTablesBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"data":[{"name":""}],"rows":1}`, branch: ListTablesBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListTables(), testConnection, ListTablesInput{Database: "analytics"})
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

func TestListTablesRejectsInvalidDatabaseAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://example.clickhouse.cloud:8443", 0)
	for _, database := range []string{"", " bad ", "bad\nname", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListTables(), testConnection, ListTablesInput{Database: database})
		require.NoError(t, err)
		require.Equal(t, ListTablesBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"data":[{"name":"events"}],"rows":1}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).ListTables(), testConnection, ListTablesInput{Database: "analytics"})
	require.NoError(t, err)
	require.Equal(t, ListTablesBranchInvalidResponse, result.Branch)
}
