// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rippling

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

var testConnection = sdkgo.ConnectionRef{Provider: "rippling", Name: "rippling"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "rippling-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-worker" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIToken: sdkgo.NewSecretString("sample-rippling-api-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetWorkerReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/workers/65699281fb7bc7c3510f44a6/", request.URL.Path)
		require.Equal(t, "Bearer sample-rippling-api-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"id":"65699281fb7bc7c3510f44a6","status":"ACTIVE","title":"Engineer","customer_id":"private"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetWorker(), testConnection, GetWorkerInput{WorkerID: "65699281fb7bc7c3510f44a6"})
	require.NoError(t, err)
	require.Equal(t, GetWorkerBranchFound, result.Branch)
	require.Equal(t, WorkerSummary{ID: "65699281fb7bc7c3510f44a6", Status: "ACTIVE", Title: "Engineer"}, result.Value)
}

func TestGetWorkerClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetWorkerBranchNotFound},
		{name: "forbidden", status: 403, branch: GetWorkerBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"id":"other","status":"ACTIVE","title":"Engineer"}`, branch: GetWorkerBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetWorker(), testConnection, GetWorkerInput{WorkerID: "65699281fb7bc7c3510f44a6"})
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

func TestGetWorkerRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://rest.ripplingapis.com", 0)
	for _, workerID := range []string{"", "../other", "bad\nworker", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetWorker(), testConnection, GetWorkerInput{WorkerID: workerID})
		require.NoError(t, err)
		require.Equal(t, GetWorkerBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"id":"65699281fb7bc7c3510f44a6","status":"ACTIVE","title":"Engineer"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).GetWorker(), testConnection, GetWorkerInput{WorkerID: "65699281fb7bc7c3510f44a6"})
	require.NoError(t, err)
	require.Equal(t, GetWorkerBranchInvalidResponse, result.Branch)
}
