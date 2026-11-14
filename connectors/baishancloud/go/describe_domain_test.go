// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package baishancloud

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

var testConnection = sdkgo.ConnectionRef{Provider: "baishancloud", Name: "baishancloud"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "baishancloud-provider-test" }
func (queryContext) StepExecutionID() string     { return "describe-domain" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, token string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {Token: sdkgo.NewSecretString(token)}})
	require.NoError(t, err)
	return client
}
func TestDescribeDomainRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v2/domain", r.URL.Path)
		require.Equal(t, "secretToken", r.URL.Query().Get("token"))
		require.Equal(t, "cdn.example.com", r.URL.Query().Get("domains"))
		require.Len(t, r.URL.Query(), 2)
		_, err := w.Write([]byte(`{"code":0,"data":[{"id":"31335","domain":"cdn.example.com","type":"download","status":"serving","cname":"cdn.example.com.qingcdn.com.","config":{"origin":{"default_master":"private.example.com"}}}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, "secretToken").DescribeDomain(), testConnection, DescribeDomainInput{Domain: "CDN.Example.Com"})
	require.NoError(t, err)
	require.Equal(t, DescribeDomainBranchFound, result.Branch)
	require.Equal(t, DomainDetail{ID: "31335", Domain: "cdn.example.com", Type: "download", Status: "serving", CNAME: "cdn.example.com.qingcdn.com."}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestDescribeDomainOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"not found", 200, `{"code":0,"data":[]}`, DescribeDomainBranchNotFound, false},
		{"token rejected", 401, `{}`, DescribeDomainBranchProviderRejected, false},
		{"provider code", 200, `{"code":42}`, DescribeDomainBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"server error", 503, `{}`, "", true},
		{"malformed", 200, `{`, DescribeDomainBranchInvalidResponse, false},
		{"wrong domain", 200, `{"code":0,"data":[{"id":"1","domain":"other.example.com","type":"page","status":"serving","cname":"x."}]}`, DescribeDomainBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, "secretToken").DescribeDomain(), testConnection, DescribeDomainInput{Domain: "cdn.example.com"})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidDomainAndTokenNeverCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, tc := range []struct {
		domain string
		token  string
	}{{"https://cdn.example.com", "secretToken"}, {"cdn.example.com", "bad token"}} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, tc.token).DescribeDomain(), testConnection, DescribeDomainInput{Domain: tc.domain})
		require.NoError(t, err)
		require.Equal(t, DescribeDomainBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardToken(t *testing.T) {
	var destinationCalls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { destinationCalls.Add(1) }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer source.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, source.URL, "privateToken").DescribeDomain(), testConnection, DescribeDomainInput{Domain: "cdn.example.com"})
	require.NoError(t, err)
	require.Equal(t, DescribeDomainBranchProviderRejected, result.Branch)
	require.Zero(t, destinationCalls.Load())
}
func TestTransportErrorDoesNotExposeToken(t *testing.T) {
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, "http://127.0.0.1:1", "privateToken").DescribeDomain(), testConnection, DescribeDomainInput{Domain: "cdn.example.com"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "privateToken")
	require.NotContains(t, err.Error(), "token=")
}
