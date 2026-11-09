// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
package azureblob

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

var testConnection = sdkgo.ConnectionRef{Provider: "microsoft-azure-blob", Name: "microsoft-azure-blob"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "azure-blob-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-blob" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AccountKey: sdkgo.NewSecretString("c2VjcmV0LXRlc3Qta2V5")}
	client, err := New(Config{AccountName: "exampleacct", Endpoint: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, signingTimeOption{now: func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }})
	require.NoError(t, err)
	return client
}
func exampleInput() GetBlobInput { return GetBlobInput{Container: "docs", BlobName: "example.txt"} }
func TestGetBlobSignsAndDownloadsBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/docs/example.txt", r.URL.Path)
		require.Equal(t, "Fri, 30 Oct 2026 00:00:00 GMT", r.Header.Get("x-ms-date"))
		require.Equal(t, "2023-11-03", r.Header.Get("x-ms-version"))
		require.Equal(t, "SharedKey exampleacct:9KwtNRsWWytJ10epCXSY4Wzg/SB1bvqQlYy8SGhDraE=", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"0x1"`)
		_, err := w.Write([]byte("hello"))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetBlob(), testConnection, exampleInput())
	require.NoError(t, err)
	require.Equal(t, GetBlobBranchDownloaded, result.Branch)
	require.Equal(t, "aGVsbG8=", result.Value.DataBase64)
	require.Equal(t, "text/plain", result.Value.ContentType)
	require.Equal(t, `"0x1"`, result.Value.ETag)
}
func TestGetBlobClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		branch sdkgo.BranchID
		retry  bool
	}{
		{"missing", 404, GetBlobBranchMissing, false}, {"forbidden", 403, GetBlobBranchProviderRejected, false},
		{"rate limit", 429, "", true}, {"unavailable", 503, "", true},
		{"unexpected partial", 206, GetBlobBranchProviderRejected, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) }))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetBlob(), testConnection, exampleInput())
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestGetBlobRejectsInvalidPathAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://exampleacct.blob.core.windows.net", 0)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetBlob(), testConnection, GetBlobInput{Container: "INVALID", BlobName: "example.txt"})
	require.NoError(t, err)
	require.Equal(t, GetBlobBranchDefect, result.Branch)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte("too large"))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 3).GetBlob(), testConnection, exampleInput())
	require.NoError(t, err)
	require.Equal(t, GetBlobBranchInvalidResponse, result.Branch)
}
