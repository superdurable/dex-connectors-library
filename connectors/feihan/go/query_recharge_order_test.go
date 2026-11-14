// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package feihan

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
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

var testConnection = sdkgo.ConnectionRef{Provider: "feihan", Name: "feihan"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "feihan-provider-test" }
func (queryContext) StepExecutionID() string     { return "recharge-order" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {ProjectID: "A12345", Password: sdkgo.NewSecretString("project-password"), KeyStr: sdkgo.NewSecretString("private-key")}})
	require.NoError(t, err)
	return client
}
func runOrder(t *testing.T, server *httptest.Server) (sdkgo.QueryResult[RechargeOrderResult], error) {
	t.Helper()
	return sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).QueryRechargeOrder(), testConnection, QueryRechargeOrderInput{MerchantOrderID: "ORDER123"})
}
func TestMD5SignatureVector(t *testing.T) {
	passwordDigest := md5.Sum([]byte("project-password"))
	require.Equal(t, "a86d4eaa08e4826bc8c166670f98bc3d", hex.EncodeToString(passwordDigest[:]))
	signatureDigest := md5.Sum([]byte("A12345" + hex.EncodeToString(passwordDigest[:]) + "ORDER123" + "private-key"))
	require.Equal(t, "14BC4ADFFC878A3453634B504819B55E", strings.ToUpper(hex.EncodeToString(signatureDigest[:])))
}
func TestQueryRechargeOrderSignedRequestAndPrivateOutput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/queryOrderInfo.do", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		require.Equal(t, "A12345", r.PostForm.Get("userid"))
		require.Equal(t, "a86d4eaa08e4826bc8c166670f98bc3d", r.PostForm.Get("userpws"))
		require.Equal(t, "ORDER123", r.PostForm.Get("sporder_id"))
		require.Equal(t, "14BC4ADFFC878A3453634B504819B55E", r.PostForm.Get("md5_str"))
		require.Equal(t, "6.0", r.PostForm.Get("version"))
		require.Equal(t, "json", r.PostForm.Get("format"))
		_, err := w.Write([]byte(`{"retcode":"1","sporder_id":"ORDER123","game_state":1,"billState":1,"game_userid":"private account","cardpws":"private card"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := runOrder(t, server)
	require.NoError(t, err)
	require.Equal(t, QueryRechargeOrderBranchFound, result.Branch)
	require.Equal(t, RechargeOrderResult{MerchantOrderID: "ORDER123", RechargeStatus: 1}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private account")
	require.NotContains(t, string(encoded), "private card")
	require.EqualValues(t, 1, calls.Load())
}
func TestQueryRechargeOrderOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"provider rejected", 200, `{"retcode":"1001","message":"bad key"}`, QueryRechargeOrderBranchProviderRejected, false},
		{"HTTP limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"bad JSON", 200, `{`, QueryRechargeOrderBranchInvalidResponse, false},
		{"missing status", 200, `{"retcode":"1","sporder_id":"ORDER123"}`, QueryRechargeOrderBranchInvalidResponse, false},
		{"unresolved order", 200, `{"retcode":"1","sporder_id":"ORDER123","game_state":-1}`, QueryRechargeOrderBranchProviderRejected, false},
		{"wrong order", 200, `{"retcode":"1","sporder_id":"OTHER","game_state":1}`, QueryRechargeOrderBranchInvalidResponse, false},
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
					require.NotContains(t, result.Failure.Message, "private-key")
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
	result, err := sdkgo.RunQuery(queryContext{}, client.QueryRechargeOrder(), testConnection, QueryRechargeOrderInput{MerchantOrderID: ""})
	require.NoError(t, err)
	require.Equal(t, QueryRechargeOrderBranchDefect, result.Branch)
	client, err = New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {ProjectID: "A12345", Password: sdkgo.NewSecretString(""), KeyStr: sdkgo.NewSecretString("private-key")}})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, client.QueryRechargeOrder(), testConnection, QueryRechargeOrderInput{MerchantOrderID: "ORDER123"})
	require.NoError(t, err)
	require.Equal(t, QueryRechargeOrderBranchDefect, result.Branch)
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
	require.Equal(t, QueryRechargeOrderBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
}
