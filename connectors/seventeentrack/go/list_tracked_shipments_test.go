// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package seventeentrack

import (
	"context"
	"encoding/json"
	"io"
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

var testConnection = sdkgo.ConnectionRef{Provider: "seventeentrack", Name: "seventeentrack"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "seventeentrack-provider-test" }
func (queryContext) StepExecutionID() string     { return "tracking-list" }
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
func TestListTrackedShipmentsRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/track/v2.4/gettracklist", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "secretTestKey", r.Header.Get("17token"))
		require.Empty(t, r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var request map[string]int
		require.NoError(t, json.Unmarshal(body, &request))
		require.Equal(t, map[string]int{"page_no": 2}, request)
		_, err = w.Write([]byte(`{"code":0,"page":{"data_total":43,"page_no":2,"page_size":40},"data":{"accepted":[{"number":"RR123456789CN","carrier":3011,"package_status":"InTransit","tracking_status":"Tracking","email":"sensitive@example.com","consignee":"Private Person","latest_event_info":"private"}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListTrackedShipments(), testConnection, ListTrackedShipmentsInput{PageNumber: 2})
	require.NoError(t, err)
	require.Equal(t, ListTrackedShipmentsBranchListed, result.Branch)
	require.Equal(t, TrackedShipmentPage{PageNumber: 2, Total: 43, Shipments: []TrackedShipment{{Number: "RR123456789CN", Carrier: 3011, PackageStatus: "InTransit", TrackingStatus: "Tracking"}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "sensitive@example.com")
	require.NotContains(t, string(encoded), "Private Person")
	require.NotContains(t, string(encoded), "private")
	require.EqualValues(t, 1, calls.Load())
}
func TestListTrackedShipmentsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"code":0,"page":{"data_total":0,"page_no":1,"page_size":40},"data":{"accepted":[]}}`, ListTrackedShipmentsBranchListed, false},
		{"provider code", 200, `{"code":-18010012,"data":{}}`, ListTrackedShipmentsBranchProviderRejected, false},
		{"provider errors", 200, `{"code":0,"data":{"errors":[{"code":-18010013}]}}`, ListTrackedShipmentsBranchProviderRejected, false},
		{"invalid token", 401, `{}`, ListTrackedShipmentsBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, ListTrackedShipmentsBranchInvalidResponse, false},
		{"missing page", 200, `{"code":0,"data":{"accepted":[]}}`, ListTrackedShipmentsBranchInvalidResponse, false},
		{"missing shipment", 200, `{"code":0,"page":{"page_no":1,"page_size":40},"data":{"accepted":[{"number":"RR123456789CN"}]}}`, ListTrackedShipmentsBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListTrackedShipments(), testConnection, ListTrackedShipmentsInput{})
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
func TestInvalidPageAndKeyNeverCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListTrackedShipments(), testConnection, ListTrackedShipmentsInput{PageNumber: -1})
	require.NoError(t, err)
	require.Equal(t, ListTrackedShipmentsBranchDefect, result.Branch)
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("bad key")}})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListTrackedShipments(), testConnection, ListTrackedShipmentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListTrackedShipmentsBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardKey(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListTrackedShipments(), testConnection, ListTrackedShipmentsInput{})
	require.NoError(t, err)
	require.Equal(t, ListTrackedShipmentsBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "secretTestKey")
	require.False(t, strings.Contains(result.Failure.Message, "17token"))
}
