// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rippling

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListWorkersReadsVisibleFieldsAndCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/workers/", request.URL.Path)
		require.Equal(t, "2", request.URL.Query().Get("limit"))
		require.Equal(t, "next-page", request.URL.Query().Get("cursor"))
		require.Equal(t, "Bearer sample-rippling-api-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"results":[{"id":"65699281fb7bc7c3510f44a6","status":"ACTIVE","title":"Engineer","personal_email":"private"}],"next_link":"https://rest.ripplingapis.com/workers/?cursor=following"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListWorkers(), testConnection, ListWorkersInput{Limit: 2, Cursor: "next-page"})
	require.NoError(t, err)
	require.Equal(t, ListWorkersBranchFound, result.Branch)
	require.Equal(t, WorkerPage{Workers: []WorkerSummary{{ID: "65699281fb7bc7c3510f44a6", Status: "ACTIVE", Title: "Engineer"}}, NextCursor: "following"}, result.Value)
}

func TestListWorkersClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "forbidden", status: 403, branch: ListWorkersBranchProviderRejected},
		{name: "rate limited", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "missing results", status: 200, body: `{}`, branch: ListWorkersBranchInvalidResponse},
		{name: "invalid worker", status: 200, body: `{"results":[{"id":"../bad"}]}`, branch: ListWorkersBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListWorkers(), testConnection, ListWorkersInput{Limit: 2})
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
