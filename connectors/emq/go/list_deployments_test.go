// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package emq

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "emq", Name: "emq"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "emq-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-deployments" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, limit int64) *Client {
	t.Helper()
	credentials := Credentials{PlatformAPIKey: sdkgo.NewSecretString("test-platform-key"), PlatformAPISecret: sdkgo.NewSecretString("test-platform-secret")}
	client, err := New(Config{PlatformEndpoint: endpoint, MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}
func TestListDeploymentsRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/public_api/v1/deployments", request.URL.Path)
		key, secret, ok := request.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "test-platform-key", key)
		require.Equal(t, "test-platform-secret", secret)
		require.Empty(t, request.URL.RawQuery)
		_, err := writer.Write([]byte(`[{"connections":1000,"tps":1000,"createAt":"2024-11-21 02:51","deploymentID":"mdfba81f","deploymentName":"deployment-mdfba81f","deploymentType":"serverless","platform":"AWS","projectName":"default","region":"N. Virginia (us-east-1)","status":"stopped","version":"v5"}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/public_api/v1", 0).ListDeployments(), testConnection, ListDeploymentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListDeploymentsBranchListed, result.Branch)
	require.Equal(t, []Deployment{{DeploymentID: "mdfba81f", DeploymentName: "deployment-mdfba81f", DeploymentType: "serverless", ProjectName: "default", Region: "N. Virginia (us-east-1)", Status: "stopped", Version: "v5", Platform: "AWS", Connections: 1000, TPS: 1000, CreatedAt: "2024-11-21 02:51"}}, result.Value.Deployments)
}
func TestListDeploymentsEmptyAndProviderFailures(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, "[]", ListDeploymentsBranchListed, false},
		{"unauthorized", 401, `{"error":"test-platform-secret"}`, ListDeploymentsBranchProviderRejected, false},
		{"forbidden", 403, "", ListDeploymentsBranchProviderRejected, false},
		{"rate limit", 429, "", "", true},
		{"unavailable", 503, "", "", true},
		{"malformed", 200, `{"deployments":[]}`, ListDeploymentsBranchInvalidResponse, false},
		{"missing ID", 200, `[{"deploymentName":"bad","deploymentType":"serverless","status":"running","connections":1,"tps":1}]`, ListDeploymentsBranchInvalidResponse, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/public_api/v1", 0).ListDeployments(), testConnection, ListDeploymentsInput{})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if testCase.name == "unauthorized" {
				require.NotContains(t, result.Failure.Message, "test-platform-secret")
			}
		})
	}
}
func TestListDeploymentsBoundsAndEndpointValidation(t *testing.T) {
	for _, endpoint := range []string{"", "http://example.com/public_api/v1", "https://example.com/public_api/v1/", "https://key:secret@example.com/public_api/v1", "https://example.com/public_api/v1?q=secret", "https://example.com/other"} {
		_, err := New(Config{PlatformEndpoint: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(strings.Repeat("x", 200)))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/public_api/v1", 64).ListDeployments(), testConnection, ListDeploymentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListDeploymentsBranchInvalidResponse, result.Branch)
}
func TestListDeploymentsTransportErrorDoesNotLeakCredentials(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-platform-secret") })
	credentials := Credentials{PlatformAPIKey: sdkgo.NewSecretString("test-platform-key"), PlatformAPISecret: sdkgo.NewSecretString("test-platform-secret")}
	client, err := New(Config{PlatformEndpoint: "https://api.example.com/public_api/v1"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	_, err = sdkgo.RunQuery(queryContext{}, client.ListDeployments(), testConnection, ListDeploymentsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-platform-secret")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
