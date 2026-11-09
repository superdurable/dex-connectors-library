// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package firestore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessToken = "synthetic-google-firestore-token"

var testConnection = sdkgo.ConnectionRef{Provider: "google-firestore", Name: "google-firestore"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "google-firestore-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	credentials := Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestListDocumentsProjectsOneCursorPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/projects/sample-project/databases/(default)/documents/events", r.URL.Path)
		require.Equal(t, "2", r.URL.Query().Get("pageSize"))
		require.Equal(t, "next page", r.URL.Query().Get("pageToken"))
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		_, err := w.Write([]byte(`{"documents":[{"name":"projects/sample-project/databases/(default)/documents/events/sample-event","labels":{"secret":"not-persisted"}}],"nextPageToken":"further"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListDocuments(), testConnection, ListDocumentsInput{ProjectID: "sample-project", CollectionID: "events", PageSize: 2, PageToken: "next page"})
	require.NoError(t, err)
	require.Equal(t, ListDocumentsBranchListed, result.Branch)
	require.Equal(t, DocumentsPage{Documents: []Document{{Name: "projects/sample-project/databases/(default)/documents/events/sample-event"}}, NextPageToken: "further"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestDecodeDocumentsPageBoundsDocumentIDBytes(t *testing.T) {
	input := ListDocumentsInput{ProjectID: "sample-project", CollectionID: "events", PageSize: 1}
	prefix := "projects/sample-project/databases/(default)/documents/events/"
	page := func(documentID string) []byte {
		encoded, err := json.Marshal(map[string]any{"documents": []map[string]string{{"name": prefix + documentID}}})
		require.NoError(t, err)
		return encoded
	}
	_, err := decodeDocumentsPage(page(strings.Repeat("\u00e9", 700)+strings.Repeat("a", 100)), input)
	require.NoError(t, err)
	_, err = decodeDocumentsPage(page(strings.Repeat("\u00e9", 751)), input)
	require.Error(t, err)
}

func TestListDocumentsClassifiesFailuresAndRejectsInvalidInputWithoutRequest(t *testing.T) {
	var responseStatus atomic.Int32
	var responseBody atomic.Value
	var calls atomic.Int32
	responseBody.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(responseStatus.Load()))
		_, err := w.Write([]byte(responseBody.Load().(string)))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		responseStatus.Store(int32(status))
		_, err := sdkgo.RunQuery(queryContext{}, client.ListDocuments(), testConnection, ListDocumentsInput{ProjectID: "sample-project", CollectionID: "events"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
	responseStatus.Store(http.StatusForbidden)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListDocuments(), testConnection, ListDocumentsInput{ProjectID: "sample-project", CollectionID: "events"})
	require.NoError(t, err)
	require.Equal(t, ListDocumentsBranchProviderRejected, result.Branch)
	responseStatus.Store(http.StatusOK)
	responseBody.Store(`{"documents":{}}`)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListDocuments(), testConnection, ListDocumentsInput{ProjectID: "sample-project", CollectionID: "events"})
	require.NoError(t, err)
	require.Equal(t, ListDocumentsBranchInvalidResponse, result.Branch)
	for _, input := range []ListDocumentsInput{{ProjectID: "sample-project", CollectionID: "events", PageSize: 101}, {ProjectID: "sample-project", CollectionID: "events", PageToken: "a\nb"}, {ProjectID: ""}} {
		result, err = sdkgo.RunQuery(queryContext{}, client.ListDocuments(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListDocumentsBranchDefect, result.Branch)
	}
	require.Equal(t, int32(4), calls.Load())
}
