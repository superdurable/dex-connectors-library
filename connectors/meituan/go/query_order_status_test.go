// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meituan

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "meituan", Name: "meituan"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "meituan-provider-test" }
func (queryContext) StepExecutionID() string     { return "order-status" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: "test-app-key", Secret: sdkgo.NewSecretString("private-secret")}})
	require.NoError(t, err)
	return client
}
func runOrder(t *testing.T, server *httptest.Server) (sdkgo.QueryResult[OrderStatusResult], error) {
	t.Helper()
	return sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).QueryOrderStatus(), testConnection, QueryOrderStatusInput{DeliveryID: 123456, MeituanOrderID: "MT123456789"})
}
func TestOfficialSHA1Vector(t *testing.T) {
	form := url.Values{"appkey": {"test"}, "timestamp": {"1477395862"}, "version": {"1.0"}, "number": {"123"}, "string": {"测试"}, "double": {"123.123"}, "boolean": {"true"}, "empty": {""}}
	require.Equal(t, "8943ba698f4b009f80dc2fd69ff9b313381263bd", signOrderStatus(form, "test"))
}
func TestQueryOrderStatusSignedRequestAndPrivateOutput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/order/status/query", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "application/x-www-form-urlencoded; charset=UTF-8", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		require.Equal(t, "test-app-key", r.PostForm.Get("appkey"))
		require.Equal(t, "1.0", r.PostForm.Get("version"))
		require.Equal(t, "123456", r.PostForm.Get("delivery_id"))
		require.Equal(t, "MT123456789", r.PostForm.Get("mt_peisong_id"))
		require.Len(t, r.PostForm.Get("timestamp"), 10)
		keys := []string{"appkey", "delivery_id", "mt_peisong_id", "timestamp", "version"}
		sort.Strings(keys)
		var signed strings.Builder
		signed.WriteString("private-secret")
		for _, key := range keys {
			signed.WriteString(key)
			signed.WriteString(r.PostForm.Get(key))
		}
		digest := sha1.Sum([]byte(signed.String()))
		require.Equal(t, hex.EncodeToString(digest[:]), r.PostForm.Get("sign"))
		_, err := w.Write([]byte(`{"code":0,"data":{"delivery_id":123456,"mt_peisong_id":"MT123456789","order_id":"ORDER123","status":20,"operate_time":1794552600,"courier_name":"private rider","courier_phone":"12345678900","temperature":"36.4"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := runOrder(t, server)
	require.NoError(t, err)
	require.Equal(t, QueryOrderStatusBranchFound, result.Branch)
	require.Equal(t, OrderStatusResult{DeliveryID: 123456, MeituanOrderID: "MT123456789", MerchantOrderID: "ORDER123", Status: 20, OperatedAt: 1794552600}, result.Value)
	encoded, _ := json.Marshal(result.Value)
	require.NotContains(t, string(encoded), "private rider")
	require.NotContains(t, string(encoded), "12345678900")
	require.NotContains(t, string(encoded), "36.4")
	require.EqualValues(t, 1, calls.Load())
}
func TestQueryOrderStatusOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"provider rejected", 200, `{"code":12,"message":"bad request"}`, QueryOrderStatusBranchProviderRejected, false},
		{"provider limit", 200, `{"code":11,"message":"接口流控"}`, "", true},
		{"HTTP limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"bad JSON", 200, `{`, QueryOrderStatusBranchInvalidResponse, false},
		{"missing status", 200, `{"code":0,"data":{"delivery_id":123456,"mt_peisong_id":"MT123456789","order_id":"ORDER123","operate_time":1794552600}}`, QueryOrderStatusBranchInvalidResponse, false},
		{"wrong order", 200, `{"code":0,"data":{"delivery_id":123456,"mt_peisong_id":"OTHER","order_id":"ORDER123","status":20,"operate_time":1794552600}}`, QueryOrderStatusBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := runOrder(t, server)
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				if result.Failure != nil {
					require.NotContains(t, result.Failure.Message, "private-secret")
				}
			}
		})
	}
}
func TestInvalidOrderAndCredentialsNeverCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.QueryOrderStatus(), testConnection, QueryOrderStatusInput{DeliveryID: 0, MeituanOrderID: "MT123456789"})
	require.NoError(t, err)
	require.Equal(t, QueryOrderStatusBranchDefect, result.Branch)
	client, err = New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: "test-app-key", Secret: sdkgo.NewSecretString("")}})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, client.QueryOrderStatus(), testConnection, QueryOrderStatusInput{DeliveryID: 123456, MeituanOrderID: "MT123456789"})
	require.NoError(t, err)
	require.Equal(t, QueryOrderStatusBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardSignedBody(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := runOrder(t, server)
	require.NoError(t, err)
	require.Equal(t, QueryOrderStatusBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
}
