// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package segment

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

var testConnection = sdkgo.ConnectionRef{Provider: "segment", Name: "segment"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "segment-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-source" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-segment-api-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetSourceReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/sources/source123", request.URL.Path)
		require.Equal(t, "Bearer sample-segment-api-key", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"data":{"source":{"id":"source123","slug":"my-source","name":"My Source","writeKeys":["secret"]}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetSource(), testConnection, GetSourceInput{SourceID: "source123"})
	require.NoError(t, err)
	require.Equal(t, GetSourceBranchFound, result.Branch)
	require.Equal(t, SourceProfile{ID: "source123", Slug: "my-source", Name: "My Source"}, result.Value)
}

func TestGetSourceClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetSourceBranchNotFound},
		{name: "forbidden", status: 403, branch: GetSourceBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"data":{"source":{"id":"other","slug":"my-source","name":"My Source"}}}`, branch: GetSourceBranchInvalidResponse},
		{name: "missing slug", status: 200, body: `{"data":{"source":{"id":"source123","name":"My Source"}}}`, branch: GetSourceBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetSource(), testConnection, GetSourceInput{SourceID: "source123"})
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

func TestGetSourceRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.segmentapis.com", 0)
	for _, sourceID := range []string{"", "../other", "bad\nsource", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetSource(), testConnection, GetSourceInput{SourceID: sourceID})
		require.NoError(t, err)
		require.Equal(t, GetSourceBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"data":{"source":{"id":"source123","slug":"my-source","name":"My Source"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 20).GetSource(), testConnection, GetSourceInput{SourceID: "source123"})
	require.NoError(t, err)
	require.Equal(t, GetSourceBranchInvalidResponse, result.Branch)
}
