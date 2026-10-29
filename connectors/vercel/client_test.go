// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vercel

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

type testContext struct{ dex.Context }

func (testContext) FlowID() string              { return "provider-test" }
func (testContext) StepExecutionID() string     { return "test" }
func (testContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (testContext) Done() <-chan struct{}       { return nil }
func (testContext) Err() error                  { return nil }
func (testContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "vercel", Name: "vercel"}

func testClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString("test-accesstoken")}})
	require.NoError(t, err)
	return client
}

func TestListDeploymentsProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/v6/deployments", r.URL.EscapedPath())
		require.Equal(t, "Bearer test-accesstoken", r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.RawQuery, "test-")
		_, err := fmt.Fprint(w, `{"deployments":[{"uid":"dpl_meridian1","name":"meridian-web","url":"meridian-web.vercel.app","state":"READY","created":1761436800000}],"pagination":{"next":null}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	result, err := sdkgo.RunQuery(testContext{}, client.ListDeployments(), testConnection, ListDeploymentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListDeploymentsBranchListed, result.Branch)
	require.Nil(t, result.Failure)
}
func TestListDeploymentsRetriesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer server.Close()
	client := testClient(t, server.URL)
	_, err := sdkgo.RunQuery(testContext{}, client.ListDeployments(), testConnection, ListDeploymentsInput{})
	require.Error(t, err)
	require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
}

func TestGetDeploymentProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/v13/deployments/dpl_meridian1", r.URL.EscapedPath())
		require.Equal(t, "Bearer test-accesstoken", r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.RawQuery, "test-")
		_, err := fmt.Fprint(w, `{"uid":"dpl_meridian1","name":"meridian-web","url":"meridian-web.vercel.app","state":"READY","created":1761436800000}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	result, err := sdkgo.RunQuery(testContext{}, client.GetDeployment(), testConnection, GetDeploymentInput{IDOrURL: "dpl_meridian1"})
	require.NoError(t, err)
	require.Equal(t, GetDeploymentBranchFound, result.Branch)
	require.Nil(t, result.Failure)
}
func TestGetDeploymentRetriesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer server.Close()
	client := testClient(t, server.URL)
	_, err := sdkgo.RunQuery(testContext{}, client.GetDeployment(), testConnection, GetDeploymentInput{IDOrURL: "dpl_meridian1"})
	require.Error(t, err)
	require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
}
