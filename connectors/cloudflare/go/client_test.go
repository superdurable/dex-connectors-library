// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudflare

import (
	"context"
	"encoding/json"
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

var testConnection = sdkgo.ConnectionRef{Provider: "cloudflare", Name: "cloudflare"}

func testClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIToken: sdkgo.NewSecretString("test-apitoken")}})
	require.NoError(t, err)
	return client
}

func TestListZonesProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/zones", r.URL.EscapedPath())
		require.Equal(t, "Bearer test-apitoken", r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.RawQuery, "test-")
		_, err := fmt.Fprint(w, `{"success":true,"result":[{"id":"zone-meridian","name":"meridian.example","status":"active"}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	result, err := sdkgo.RunQuery(testContext{}, client.ListZones(), testConnection, ListZonesInput{})
	require.NoError(t, err)
	require.Equal(t, ListZonesBranchListed, result.Branch)
	require.Nil(t, result.Failure)
}
func TestListZonesRetriesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer server.Close()
	client := testClient(t, server.URL)
	_, err := sdkgo.RunQuery(testContext{}, client.ListZones(), testConnection, ListZonesInput{})
	require.Error(t, err)
	require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
}

func TestPurgeCacheByURLsProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/zones/zone-meridian/purge_cache", r.URL.EscapedPath())
		require.Equal(t, "Bearer test-apitoken", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var payload struct {
			Files []string `json:"files"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, []string{"https://meridian.example/index.html"}, payload.Files)
		require.NotContains(t, r.URL.RawQuery, "test-")
		_, err := fmt.Fprint(w, `{"success":true,"result":{"id":"purge-meridian"}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	result, err := sdkgo.RunMutation(testContext{}, client.PurgeCacheByURLs(), testConnection, PurgeCacheByURLsInput{ZoneID: "zone-meridian", URLs: []string{"https://meridian.example/index.html"}})
	require.NoError(t, err)
	require.Equal(t, PurgeCacheByURLsBranchPurged, result.Branch)
	require.Nil(t, result.Failure)
}

func TestPurgeCacheByURLsClassifiesProviderRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := fmt.Fprint(w, `{"success":false,"errors":[{"message":"SENTINEL"}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	result, err := sdkgo.RunMutation(testContext{}, client.PurgeCacheByURLs(), testConnection, PurgeCacheByURLsInput{
		ZoneID: "zone-meridian", URLs: []string{"https://meridian.example/index.html"},
	})
	require.NoError(t, err)
	require.Equal(t, PurgeCacheByURLsBranchProviderRejected, result.Branch)
	require.NotContains(t, result.Failure.Message, "SENTINEL")
}
func TestPurgeCacheByURLsRetriesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer server.Close()
	client := testClient(t, server.URL)
	_, err := sdkgo.RunMutation(testContext{}, client.PurgeCacheByURLs(), testConnection, PurgeCacheByURLsInput{ZoneID: "zone-meridian", URLs: []string{"https://meridian.example/index.html"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
}
