// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pandadoc

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

var testConnection = sdkgo.ConnectionRef{Provider: "pandadoc", Name: "pandadoc"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "pandadoc-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-document" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-pandadoc-api-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetDocumentReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/documents/fANp5wW5EwY7AkiTkTGekZ", request.URL.Path)
		require.Equal(t, "API-Key sample-pandadoc-api-key", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"id":"fANp5wW5EwY7AkiTkTGekZ","name":"Example agreement","status":"document.completed","recipients":[{"email":"private@example.com"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetDocument(), testConnection, GetDocumentInput{DocumentID: "fANp5wW5EwY7AkiTkTGekZ"})
	require.NoError(t, err)
	require.Equal(t, GetDocumentBranchFound, result.Branch)
	require.Equal(t, DocumentStatus{ID: "fANp5wW5EwY7AkiTkTGekZ", Name: "Example agreement", Status: "document.completed"}, result.Value)
}

func TestGetDocumentClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetDocumentBranchNotFound},
		{name: "forbidden", status: 403, branch: GetDocumentBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"id":"fANp5wW5EwY7AkiTkTGekZ","name":"Example agreement"}`, branch: GetDocumentBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetDocument(), testConnection, GetDocumentInput{DocumentID: "fANp5wW5EwY7AkiTkTGekZ"})
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

func TestGetDocumentRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.pandadoc.com/public/v1", 0)
	for _, documentID := range []string{"", "../other", "bad\ndocument", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetDocument(), testConnection, GetDocumentInput{DocumentID: documentID})
		require.NoError(t, err)
		require.Equal(t, GetDocumentBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"id":"fANp5wW5EwY7AkiTkTGekZ","name":"Example agreement","status":"document.completed"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).GetDocument(), testConnection, GetDocumentInput{DocumentID: "fANp5wW5EwY7AkiTkTGekZ"})
	require.NoError(t, err)
	require.Equal(t, GetDocumentBranchInvalidResponse, result.Branch)
}
