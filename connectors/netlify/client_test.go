// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package netlify

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

var testConnection = sdkgo.ConnectionRef{Provider: "netlify", Name: "netlify"}

func testClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString("test-accesstoken")}})
	require.NoError(t, err)
	return client
}

func TestListSitesProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/sites", r.URL.EscapedPath())
		require.Equal(t, "Bearer test-accesstoken", r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.RawQuery, "test-")
		_, err := fmt.Fprint(w, `[{"id":"site-meridian","name":"meridian-web","url":"https://meridian.netlify.app","admin_url":"https://app.netlify.com/sites/meridian-web"}]`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	result, err := sdkgo.RunQuery(testContext{}, client.ListSites(), testConnection, ListSitesInput{})
	require.NoError(t, err)
	require.Equal(t, ListSitesBranchListed, result.Branch)
	require.Nil(t, result.Failure)
}
func TestListSitesRetriesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer server.Close()
	client := testClient(t, server.URL)
	_, err := sdkgo.RunQuery(testContext{}, client.ListSites(), testConnection, ListSitesInput{})
	require.Error(t, err)
	require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
}

func TestListDeploysProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/sites/site-meridian/deploys", r.URL.EscapedPath())
		require.Equal(t, "Bearer test-accesstoken", r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.RawQuery, "test-")
		_, err := fmt.Fprint(w, `[{"id":"deploy-meridian","site_id":"site-meridian","state":"ready","url":"https://meridian.netlify.app"}]`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	result, err := sdkgo.RunQuery(testContext{}, client.ListDeploys(), testConnection, ListDeploysInput{SiteID: "site-meridian"})
	require.NoError(t, err)
	require.Equal(t, ListDeploysBranchListed, result.Branch)
	require.Nil(t, result.Failure)
}
func TestListDeploysRetriesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer server.Close()
	client := testClient(t, server.URL)
	_, err := sdkgo.RunQuery(testContext{}, client.ListDeploys(), testConnection, ListDeploysInput{SiteID: "site-meridian"})
	require.Error(t, err)
	require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
}
