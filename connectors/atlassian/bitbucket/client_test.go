// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "bitbucket", Name: "bitbucket"}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: Credentials{AccessToken: sdkgo.NewSecretString("test-token")}})
	require.NoError(t, err)
	return client
}

func TestListRepositoriesReadsAndClassifiesProviderStatus(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.RawQuery, "test-token")
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/2.0/repositories/meridian", r.URL.Path)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, err := fmt.Fprint(w, `{"error":"SENTINEL"}`)
			require.NoError(t, err)
			return
		}
		_, err := fmt.Fprint(w, "{\"values\":[{\"uuid\":\"{a1}\",\"name\":\"Meridian API\",\"full_name\":\"meridian/api\",\"is_private\":true}],\"page\":1,\"pagelen\":10}")
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/2.0")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListRepositories(), testConnection, ListRepositoriesInput{Workspace: "meridian"})
	require.NoError(t, err)
	require.Equal(t, ListRepositoriesBranchListed, result.Branch)
	require.Nil(t, result.Failure)
	require.Len(t, result.Value.Values, 1)
	status = http.StatusNotFound
	rejected, err := sdkgo.RunQuery(queryContext{}, client.ListRepositories(), testConnection, ListRepositoriesInput{Workspace: "meridian"})
	require.NoError(t, err)
	require.Equal(t, ListRepositoriesBranchNotFound, rejected.Branch)
	require.NotContains(t, rejected.Failure.Message, "SENTINEL")
}

func TestListPullRequestsReadsAndClassifiesProviderStatus(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.RawQuery, "test-token")
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/2.0/repositories/meridian/api/pullrequests", r.URL.Path)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, err := fmt.Fprint(w, `{"error":"SENTINEL"}`)
			require.NoError(t, err)
			return
		}
		_, err := fmt.Fprint(w, "{\"values\":[{\"id\":7,\"title\":\"Add client tests\",\"state\":\"OPEN\"}],\"page\":1,\"pagelen\":10}")
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/2.0")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListPullRequests(), testConnection, ListPullRequestsInput{Workspace: "meridian", Repository: "api"})
	require.NoError(t, err)
	require.Equal(t, ListPullRequestsBranchListed, result.Branch)
	require.Nil(t, result.Failure)
	require.Len(t, result.Value.Values, 1)
	status = http.StatusNotFound
	rejected, err := sdkgo.RunQuery(queryContext{}, client.ListPullRequests(), testConnection, ListPullRequestsInput{Workspace: "meridian", Repository: "api"})
	require.NoError(t, err)
	require.Equal(t, ListPullRequestsBranchNotFound, rejected.Branch)
	require.NotContains(t, rejected.Failure.Message, "SENTINEL")
}
