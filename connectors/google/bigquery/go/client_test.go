// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bigquery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessToken = "synthetic-google-bigquery-token"

var testConnection = sdkgo.ConnectionRef{Provider: "google-bigquery", Name: "google-bigquery"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "google-bigquery-provider-test" }
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

func TestListDatasetsProjectsOneCursorPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/bigquery/v2/projects/sample-project/datasets", r.URL.Path)
		require.Equal(t, "2", r.URL.Query().Get("maxResults"))
		require.Equal(t, "next page", r.URL.Query().Get("pageToken"))
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		_, err := w.Write([]byte(`{"datasets":[{"datasetReference":{"datasetId":"sample_dataset","projectId":"sample-project"},"friendlyName":"Sample Dataset","labels":{"secret":"not-persisted"}}],"nextPageToken":"further"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListDatasets(), testConnection, ListDatasetsInput{ProjectID: "sample-project", MaxResults: 2, PageToken: "next page"})
	require.NoError(t, err)
	require.Equal(t, ListDatasetsBranchListed, result.Branch)
	require.Equal(t, DatasetsPage{Datasets: []Dataset{{ID: "sample_dataset", ProjectID: "sample-project", FriendlyName: "Sample Dataset"}}, NextPageToken: "further"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestListDatasetsClassifiesFailuresAndRejectsInvalidInputWithoutRequest(t *testing.T) {
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
		_, err := sdkgo.RunQuery(queryContext{}, client.ListDatasets(), testConnection, ListDatasetsInput{ProjectID: "sample-project"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
	responseStatus.Store(http.StatusForbidden)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListDatasets(), testConnection, ListDatasetsInput{ProjectID: "sample-project"})
	require.NoError(t, err)
	require.Equal(t, ListDatasetsBranchProviderRejected, result.Branch)
	responseStatus.Store(http.StatusOK)
	responseBody.Store(`{"datasets":{}}`)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListDatasets(), testConnection, ListDatasetsInput{ProjectID: "sample-project"})
	require.NoError(t, err)
	require.Equal(t, ListDatasetsBranchInvalidResponse, result.Branch)
	for _, input := range []ListDatasetsInput{{ProjectID: "sample-project", MaxResults: 101}, {ProjectID: "sample-project", PageToken: "a\nb"}, {ProjectID: ""}} {
		result, err = sdkgo.RunQuery(queryContext{}, client.ListDatasets(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListDatasetsBranchDefect, result.Branch)
	}
	require.Equal(t, int32(4), calls.Load())
}
