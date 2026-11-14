// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zhihu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "zhihu", Name: "zhihu"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "zhihu-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-quotas" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, url string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: url}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessSecret: sdkgo.NewSecretString("secretTestToken")}})
	require.NoError(t, err)
	return client
}
func TestListQuotasRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/v1/quota", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "Bearer secretTestToken", r.Header.Get("Authorization"))
		timestamp, err := strconv.ParseInt(r.Header.Get("X-Request-Timestamp"), 10, 64)
		require.NoError(t, err)
		require.InDelta(t, time.Now().Unix(), timestamp, 5)
		_, err = w.Write([]byte(`{"Code":0,"Message":"success","Data":[{"APIID":"knowledge","APIName":"Knowledge Base","TotalQuota":500,"TotalUsed":12,"RemainingQuota":488}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListQuotas(), testConnection, ListQuotasInput{})
	require.NoError(t, err)
	require.Equal(t, ListQuotasBranchListed, result.Branch)
	require.Equal(t, Quota{APIID: "knowledge", APIName: "Knowledge Base", TotalQuota: 500, TotalUsed: 12, RemainingQuota: 488}, result.Value.Quotas[0])
	require.EqualValues(t, 1, calls.Load())
}
func TestListQuotasOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"Code":0,"Data":[]}`, ListQuotasBranchListed, false},
		{"invalid token", 401, `{}`, ListQuotasBranchProviderRejected, false},
		{"forbidden", 403, `{}`, ListQuotasBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"body rate limit", 200, `{"Code":30001,"Message":"rate limited"}`, "", true},
		{"body availability", 200, `{"Code":90001,"Message":"busy"}`, "", true},
		{"body authentication", 200, `{"Code":20001,"Message":"invalid secret"}`, ListQuotasBranchProviderRejected, false},
		{"body validation", 200, `{"Code":10001,"Message":"invalid APIIDs"}`, ListQuotasBranchProviderRejected, false},
		{"malformed", 200, `{`, ListQuotasBranchInvalidResponse, false},
		{"missing data", 200, `{"Code":0}`, ListQuotasBranchInvalidResponse, false},
		{"missing field", 200, `{"Code":0,"Data":[{"APIID":"knowledge","APIName":"Knowledge Base"}]}`, ListQuotasBranchInvalidResponse, false},
		{"negative quota", 200, `{"Code":0,"Data":[{"APIID":"knowledge","APIName":"Knowledge Base","TotalQuota":-1,"TotalUsed":0,"RemainingQuota":0}]}`, ListQuotasBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListQuotas(), testConnection, ListQuotasInput{})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidSecretNeverCallsProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessSecret: sdkgo.NewSecretString("Bearer bad")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListQuotas(), testConnection, ListQuotasInput{})
	require.NoError(t, err)
	require.Equal(t, ListQuotasBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardSecret(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListQuotas(), testConnection, ListQuotasInput{})
	require.NoError(t, err)
	require.Equal(t, ListQuotasBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "secretTestToken")
	require.False(t, strings.Contains(result.Failure.Message, "Bearer"))
}
