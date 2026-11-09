// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package productboard

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

var testConnection = sdkgo.ConnectionRef{Provider: "productboard", Name: "productboard"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "productboard-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-features" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIToken: sdkgo.NewSecretString("sample-productboard-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestListFeaturesReadsSelectedFieldsAndCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/entities", request.URL.Path)
		require.Equal(t, "feature", request.URL.Query().Get("type[]"))
		require.Equal(t, "name", request.URL.Query().Get("fields[]"))
		require.Equal(t, "cursor-1", request.URL.Query().Get("pageCursor"))
		require.Equal(t, "Bearer sample-productboard-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"data":[{"id":"feature-1","type":"feature","fields":{"name":"Example feature","description":"private"}}],"links":{"next":"https://api.productboard.com/v2/entities?pageCursor=cursor-2"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListFeatures(), testConnection, ListFeaturesInput{PageCursor: "cursor-1"})
	require.NoError(t, err)
	require.Equal(t, ListFeaturesBranchListed, result.Branch)
	require.Equal(t, FeaturesPage{Features: []FeatureMetadata{{ID: "feature-1", Name: "Example feature"}}, NextCursor: "cursor-2"}, result.Value)
}

func TestListFeaturesAcceptsNextLinkBelowConfiguredBase(t *testing.T) {
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"data":[],"links":{"next":"` + serverURL + `/v2/entities?pageCursor=cursor-2"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	serverURL = server.URL
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/v2", 0).ListFeatures(), testConnection, ListFeaturesInput{})
	require.NoError(t, err)
	require.Equal(t, ListFeaturesBranchListed, result.Branch)
	require.Equal(t, "cursor-2", result.Value.NextCursor)
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/v3", 0).ListFeatures(), testConnection, ListFeaturesInput{})
	require.NoError(t, err)
	require.Equal(t, ListFeaturesBranchInvalidResponse, result.Branch)
}

func TestListFeaturesClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "forbidden", status: 403, branch: ListFeaturesBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "missing data", status: 200, body: `{}`, branch: ListFeaturesBranchInvalidResponse},
		{name: "foreign pagination URL", status: 200, body: `{"data":[],"links":{"next":"https://example.com/v2/entities?pageCursor=cursor-2"}}`, branch: ListFeaturesBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListFeatures(), testConnection, ListFeaturesInput{})
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

func TestListFeaturesRejectsInvalidCursorAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.productboard.com/v2", 0)
	for _, cursor := range []string{"bad\nvalue", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListFeatures(), testConnection, ListFeaturesInput{PageCursor: cursor})
		require.NoError(t, err)
		require.Equal(t, ListFeaturesBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"data":[{"id":"feature-1","type":"feature","fields":{"name":"Example feature"}}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).ListFeatures(), testConnection, ListFeaturesInput{})
	require.NoError(t, err)
	require.Equal(t, ListFeaturesBranchInvalidResponse, result.Branch)
}
