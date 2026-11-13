// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zadig

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "private-test-key"

var testConnection = sdkgo.ConnectionRef{Provider: "zadig", Name: "zadig"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "zadig-provider-test" }
func (queryContext) StepExecutionID() string     { return "project-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	client, err := New(Config{BaseURL: baseURL, MaxResponseBytes: maxResponseBytes}, credentials)
	require.NoError(t, err)
	return client
}
func TestProjectCatalogRequestAndPrivacy(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/openapi/projects/project", request.URL.Path)
		require.Equal(t, "2", request.URL.Query().Get("pageNum"))
		require.Equal(t, "10", request.URL.Query().Get("pageSize"))
		require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"total":25,"projects":[{"project_key":"demo","project_name":"Demo","deploy_type":"yaml","description":"private"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListProjects(), testConnection, ListProjectsInput{PageNum: 2, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, ListProjectsBranchListed, result.Branch)
	require.Equal(t, ListProjectsOutput{Projects: []Project{{Key: "demo", Name: "Demo", DeployType: "yaml"}}, Total: 25}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestProjectCatalogOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"unauthorized", 401, `{"error":{"code":"UNAUTHORIZED","message":"private-test-key"}}`, ListProjectsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, `private`, ListProjectsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"bad request", 400, `private`, ListProjectsBranchProviderRejected, sdkgo.FailureProviderRejection, false},

		{"rate limited", 429, `private`, "", sdkgo.FailureRateLimit, true},
		{"bad gateway", 502, `private`, "", sdkgo.FailureAvailability, true},
		{"malformed", 200, `{`, ListProjectsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing data", 200, `{"total":1}`, ListProjectsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing project key", 200, `{"total":1,"projects":[{"project_name":"Demo"}]}`, ListProjectsBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListProjects(), testConnection, ListProjectsInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAPIKey)
			}
			if err != nil {
				require.NotContains(t, err.Error(), testAPIKey)
			}
		})
	}
}
func TestProjectCatalogSizeAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"total":1,"projects":[{"project_key":"demo","project_name":"Demo","deploy_type":"yaml"}]}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).ListProjects(), testConnection, ListProjectsInput{})
	require.NoError(t, err)
	require.Equal(t, ListProjectsBranchInvalidResponse, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListProjects(), testConnection, ListProjectsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIKey)
}

func TestProductionBaseURLRequiresHTTPS(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	_, err := New(Config{BaseURL: "http://zadig.example.com"}, credentials)
	require.ErrorContains(t, err, "HTTPS")
}

func TestBaseURLRejectsPath(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{}
	for _, baseURL := range []string{"https://zadig.example.com/api", "https://zadig.example.com/api/", "https://zadig.example.com/a/b"} {
		_, err := New(Config{BaseURL: baseURL}, credentials)
		require.ErrorContains(t, err, "HTTPS origin without a path", baseURL)
	}
	for _, baseURL := range []string{"https://zadig.example.com", "https://zadig.example.com/"} {
		client, err := New(Config{BaseURL: baseURL}, credentials)
		require.NoError(t, err, baseURL)
		require.Equal(t, "https://zadig.example.com", client.baseURL)
	}
}
