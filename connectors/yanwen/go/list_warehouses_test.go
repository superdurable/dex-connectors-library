// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package yanwen

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
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

var testConnection = sdkgo.ConnectionRef{Provider: "yanwen", Name: "yanwen"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "yanwen-provider-test" }
func (queryContext) StepExecutionID() string     { return "warehouse-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {UserID: "customer-1", APIToken: sdkgo.NewSecretString("private-token")}})
	require.NoError(t, err)
	return client
}
func runWarehouses(t *testing.T, server *httptest.Server) (sdkgo.QueryResult[WarehouseList], error) {
	t.Helper()
	return sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListWarehouses(), testConnection, ListWarehousesInput{})
}
func TestListWarehousesSignedRequestAndBoundedOutput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/order", r.URL.Path)
		require.Equal(t, "customer-1", r.URL.Query().Get("user_id"))
		require.Equal(t, "common.warehouse.getlist", r.URL.Query().Get("method"))
		require.Equal(t, "json", r.URL.Query().Get("format"))
		require.Equal(t, "V1.0", r.URL.Query().Get("version"))
		require.Equal(t, "application/json; charset=UTF-8", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "{}", string(body))
		timestamp := r.URL.Query().Get("timestamp")
		require.Len(t, timestamp, 13)
		_, err = strconv.ParseInt(timestamp, 10, 64)
		require.NoError(t, err)
		signed := "private-token" + "customer-1" + "{}" + "json" + "common.warehouse.getlist" + timestamp + "V1.0" + "private-token"
		digest := md5.Sum([]byte(signed))
		require.Equal(t, hex.EncodeToString(digest[:]), r.URL.Query().Get("sign"))
		require.NotContains(t, r.URL.String(), "private-token")
		_, err = w.Write([]byte(`{"success":true,"code":"0","data":[{"code":"01","name":"北京燕文","area":"华北","phone":"private-phone"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := runWarehouses(t, server)
	require.NoError(t, err)
	require.Equal(t, ListWarehousesBranchListed, result.Branch)
	require.Equal(t, []Warehouse{{Code: "01", Name: "北京燕文", Area: "华北"}}, result.Value.Warehouses)
	encoded, _ := json.Marshal(result.Value)
	require.NotContains(t, string(encoded), "private-phone")
	require.EqualValues(t, 1, calls.Load())
}
func TestWarehouseSignatureUsesCompactBody(t *testing.T) {
	token := "example-token"
	require.Equal(t, "45b1a819001f273e81923ed0f4efab7e", signWarehouseRequest(token, "100000", "{}", "1655798809414"))
}
func TestListWarehousesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"success":true,"code":"0","data":[]}`, ListWarehousesBranchListed, false},
		{"rejected", 200, `{"success":false,"code":"1","message":"bad token"}`, ListWarehousesBranchProviderRejected, false},
		{"system error", 200, `{"success":false,"code":"-1","message":"unavailable"}`, "", true},
		{"HTTP limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, ListWarehousesBranchInvalidResponse, false},
		{"missing data", 200, `{"success":true,"code":"0"}`, ListWarehousesBranchInvalidResponse, false},
		{"missing warehouse", 200, `{"success":true,"code":"0","data":[{"code":"01"}]}`, ListWarehousesBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := runWarehouses(t, server)
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				if result.Failure != nil {
					require.NotContains(t, result.Failure.Message, "private-token")
				}
			}
		})
	}
}
func TestMissingCredentialsNeverCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {UserID: "customer-1", APIToken: sdkgo.NewSecretString("")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListWarehouses(), testConnection, ListWarehousesInput{})
	require.NoError(t, err)
	require.Equal(t, ListWarehousesBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardSignature(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := runWarehouses(t, server)
	require.NoError(t, err)
	require.Equal(t, ListWarehousesBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.False(t, strings.Contains(result.Failure.Message, "private-token"))
}
