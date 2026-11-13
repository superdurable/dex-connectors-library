// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fiftyonetracking

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "fiftyonetracking", Name: "fiftyonetracking"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "51tracking-provider-test" }
func (queryContext) StepExecutionID() string     { return "status-counts" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("secretTestKey")}})
	require.NoError(t, err)
	return client
}
func TestGetShipmentStatusCountsRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v3/trackings/status", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "secretTestKey", r.Header.Get("Tracking-Api-Key"))
		require.Empty(t, r.Header.Get("Authorization"))
		_, err := w.Write([]byte(`{"code":200,"message":"Request response is successful","data":{"pending":1,"notfound":2,"transit":3,"pickup":4,"delivered":5,"expired":6,"undelivered":7,"exception":8,"email":"private@example.com"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetShipmentStatusCounts(), testConnection, GetShipmentStatusCountsInput{})
	require.NoError(t, err)
	require.Equal(t, GetShipmentStatusCountsBranchCounted, result.Branch)
	require.Equal(t, ShipmentStatusCounts{Pending: 1, NotFound: 2, InTransit: 3, Pickup: 4, Delivered: 5, Expired: 6, Undelivered: 7, Exception: 8}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestGetShipmentStatusCountsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"code":200,"data":{"pending":0,"notfound":0,"transit":0,"pickup":0,"delivered":0,"expired":0,"undelivered":0,"exception":0}}`, GetShipmentStatusCountsBranchCounted, false},
		{"provider code", 200, `{"code":401,"message":"invalid key"}`, GetShipmentStatusCountsBranchProviderRejected, false},
		{"invalid key", 401, `{}`, GetShipmentStatusCountsBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, GetShipmentStatusCountsBranchInvalidResponse, false},
		{"missing count", 200, `{"code":200,"data":{"pending":1}}`, GetShipmentStatusCountsBranchInvalidResponse, false},
		{"negative count", 200, `{"code":200,"data":{"pending":-1,"notfound":0,"transit":0,"pickup":0,"delivered":0,"expired":0,"undelivered":0,"exception":0}}`, GetShipmentStatusCountsBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetShipmentStatusCounts(), testConnection, GetShipmentStatusCountsInput{})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				if result.Failure != nil {
					require.NotContains(t, result.Failure.Message, "secretTestKey")
				}
			}
		})
	}
}
func TestInvalidKeyNeverCallsProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("bad key")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetShipmentStatusCounts(), testConnection, GetShipmentStatusCountsInput{})
	require.NoError(t, err)
	require.Equal(t, GetShipmentStatusCountsBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardKey(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetShipmentStatusCounts(), testConnection, GetShipmentStatusCountsInput{})
	require.NoError(t, err)
	require.Equal(t, GetShipmentStatusCountsBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "secretTestKey")
	require.False(t, strings.Contains(result.Failure.Message, "Tracking-Api-Key"))
}
