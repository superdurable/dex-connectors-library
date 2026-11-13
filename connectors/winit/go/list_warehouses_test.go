// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package winit

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "winit", Name: "winit"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "winit-provider-test" }
func (queryContext) StepExecutionID() string     { return "warehouse-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint, Platform: "OWNERERP"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString("sellerTest"), Token: sdkgo.NewSecretString("tokenTest"), ClientID: sdkgo.NewSecretString("clientTest"), ClientSecret: sdkgo.NewSecretString("clientSecretTest")}})
	require.NoError(t, err)
	return client
}
func TestWinitSignatureCanonicalization(t *testing.T) {
	parameters := map[string]string{"version": "1.0", "action": "queryWarehouse", "app_key": "sellerTest", "format": "json", "platform": "OWNERERP", "sign_method": "md5", "timestamp": "2026-11-13 12:00:00"}
	canonical := "tokenTestactionqueryWarehouseapp_keysellerTestformatjsonplatformOWNERERPsign_methodmd5timestamp2026-11-13 12:00:00version1.0tokenTest"
	digest := md5.Sum([]byte(canonical))
	expected := strings.ToUpper(hex.EncodeToString(digest[:]))
	require.Equal(t, expected, signWinit(parameters, "tokenTest"))
	require.NotEqual(t, expected, signWinit(parameters, "clientSecretTest"))
}
func TestListWarehousesRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/openapi/service", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "queryWarehouse", body["action"])
		require.Equal(t, "sellerTest", body["app_key"])
		require.Equal(t, "clientTest", body["client_id"])
		require.Equal(t, "OWNERERP", body["platform"])
		parameters := map[string]string{"action": body["action"], "app_key": body["app_key"], "format": body["format"], "platform": body["platform"], "sign_method": body["sign_method"], "timestamp": body["timestamp"], "version": body["version"]}
		require.Equal(t, signWinit(parameters, "tokenTest"), body["sign"])
		require.Equal(t, signWinit(parameters, "clientSecretTest"), body["client_sign"])
		require.NotContains(t, string(body["sign"]), "tokenTest")
		_, err := w.Write([]byte(`{"code":0,"data":[{"warehouseID":1030042,"warehouseCode":"AUME","warehouseName":"AU Warehouse","warehouseAddress":"excluded"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListWarehouses(), testConnection, ListWarehousesInput{})
	require.NoError(t, err)
	require.Equal(t, ListWarehousesBranchListed, result.Branch)
	require.Equal(t, Warehouse{ID: 1030042, Code: "AUME", Name: "AU Warehouse"}, result.Value.Warehouses[0])
	require.EqualValues(t, 1, calls.Load())
}
func TestListWarehousesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"code":0,"data":[]}`, ListWarehousesBranchListed, false},
		{"provider rejected", 200, `{"code":1001,"msg":"invalid token"}`, ListWarehousesBranchProviderRejected, false},
		{"http auth", 401, `{}`, ListWarehousesBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, ListWarehousesBranchInvalidResponse, false},
		{"missing data", 200, `{"code":0}`, ListWarehousesBranchInvalidResponse, false},
		{"missing warehouse field", 200, `{"code":0,"data":[{"warehouseID":1,"warehouseCode":"AUME"}]}`, ListWarehousesBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListWarehouses(), testConnection, ListWarehousesInput{})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidCredentialNeverCallsProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, Platform: "OWNERERP"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString("sellerTest"), Token: sdkgo.NewSecretString("bad token"), ClientID: sdkgo.NewSecretString("clientTest"), ClientSecret: sdkgo.NewSecretString("clientSecretTest")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListWarehouses(), testConnection, ListWarehousesInput{})
	require.NoError(t, err)
	require.Equal(t, ListWarehousesBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListWarehouses(), testConnection, ListWarehousesInput{})
	require.NoError(t, err)
	require.Equal(t, ListWarehousesBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "tokenTest")
}
