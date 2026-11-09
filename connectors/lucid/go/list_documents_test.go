// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lucid

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "lucid", Name: "lucid"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "lucid-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-documents" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-lucid-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestListDocumentsReadsSelectedFieldsAndPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/documents/search", request.URL.Path)
		require.Equal(t, "200", request.URL.Query().Get("pageSize"))
		require.Equal(t, "cursor-1", request.URL.Query().Get("pageToken"))
		require.Equal(t, "Bearer sample-lucid-key", request.Header.Get("Authorization"))
		require.Equal(t, "1", request.Header.Get("Lucid-Api-Version"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"excludeTrashed":true}`, string(body))
		writer.Header().Set("Link", `<https://api.lucid.co/v1/documents/search?pageSize=200&pageToken=cursor-2>; rel="next"`)
		_, err = writer.Write([]byte(`[{"documentId":"110808fd-4553-4316-bccf-4f25ff59a532","title":"Example document","owner":{"name":"private"}}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListDocuments(), testConnection, ListDocumentsInput{PageToken: "cursor-1"})
	require.NoError(t, err)
	require.Equal(t, ListDocumentsBranchListed, result.Branch)
	require.Equal(t, DocumentsPage{Documents: []DocumentMetadata{{ID: "110808fd-4553-4316-bccf-4f25ff59a532", Title: "Example document"}}, NextPageToken: "cursor-2"}, result.Value)
}

func TestListDocumentsClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		link   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "forbidden", status: 403, branch: ListDocumentsBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "missing title", status: 200, body: `[{"documentId":"id"}]`, branch: ListDocumentsBranchInvalidResponse},
		{name: "foreign link", status: 200, body: `[]`, link: `<https://example.com/documents/search?pageToken=cursor>; rel="next"`, branch: ListDocumentsBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Link", testCase.link)
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListDocuments(), testConnection, ListDocumentsInput{})
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

func TestListDocumentsRejectsInvalidTokenAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.lucid.co/v1", 0)
	for _, token := range []string{"bad\ntoken", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListDocuments(), testConnection, ListDocumentsInput{PageToken: token})
		require.NoError(t, err)
		require.Equal(t, ListDocumentsBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`[{"documentId":"id","title":"Example"}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).ListDocuments(), testConnection, ListDocumentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListDocumentsBranchInvalidResponse, result.Branch)
}
