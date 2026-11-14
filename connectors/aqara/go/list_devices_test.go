// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package aqara

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "aqara", Name: "aqara"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "aqara-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-devices" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, url string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: url}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: "app-id", KeyID: "key-id", AppKey: sdkgo.NewSecretString("app-key"), AccessToken: sdkgo.NewSecretString("access-token")}})
	require.NoError(t, err)
	return client
}
func TestOfficialSigningVector(t *testing.T) {
	require.Equal(t, "bfd8dd0e7c108353e6740d81e05982d8", signDeviceRequest("532cad73c5493193d63d367016b98b27", "4e693d54d75db580a56d1263", "78784564654feda454557", "C6wuzd0Qguxzelhb", "1618914078668", "gU7Qtxi4dWnYAdmudyxni52bWZ58b8uN"))
}
func TestListDevicesSignedRequestAndPage(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v3.0/open/api", r.URL.Path)
		for header, want := range map[string]string{"Accesstoken": "access-token", "Appid": "app-id", "Keyid": "key-id"} {
			require.Equal(t, want, r.Header.Get(header))
		}
		require.Equal(t, signDeviceRequest("access-token", "app-id", "key-id", r.Header.Get("Nonce"), r.Header.Get("Time"), "app-key"), r.Header.Get("Sign"))
		_, err := strconv.ParseInt(r.Header.Get("Time"), 10, 64)
		require.NoError(t, err)
		require.JSONEq(t, `{"intent":"query.device.info","data":{"pageNum":2,"pageSize":25}}`, readBody(t, r))
		_, err = w.Write([]byte(`{"code":0,"message":"Success","requestId":"abc","result":{"data":[{"did":"lumi.1","model":"lumi.gateway.aqhm01","deviceName":"Hub","state":1,"modelType":1,"firmwareVersion":"3.2.6"}],"totalCount":30}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListDevices(), testConnection, ListDevicesInput{PageNum: 2, PageSize: 25})
	require.NoError(t, err)
	require.Equal(t, ListDevicesBranchListed, result.Branch)
	require.Equal(t, 30, result.Value.TotalCount)
	require.True(t, result.Value.Devices[0].Online)
	require.EqualValues(t, 1, calls.Load())
}
func TestListDevicesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"authentication", 401, `{}`, ListDevicesBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"provider rejection", 200, `{"code":604,"message":"invalid device"}`, ListDevicesBranchProviderRejected, false},
		{"expired token", 200, `{"code":2005,"message":"expired"}`, ListDevicesBranchProviderRejected, false},
		{"body rate limit", 200, `{"code":429,"message":"too many"}`, "", true},
		{"body server busy", 200, `{"code":104,"message":"busy"}`, "", true},
		{"malformed", 200, `{`, ListDevicesBranchInvalidResponse, false},
		{"missing devices", 200, `{"code":0,"result":{"totalCount":1}}`, ListDevicesBranchInvalidResponse, false},
		{"missing state", 200, `{"code":0,"result":{"data":[{"did":"lumi.1","model":"model"}],"totalCount":1}}`, ListDevicesBranchInvalidResponse, false},
		{"invalid state", 200, `{"code":0,"result":{"data":[{"did":"lumi.1","model":"model","state":4}],"totalCount":1}}`, ListDevicesBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListDevices(), testConnection, ListDevicesInput{})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidPageNeverCallsProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListDevices(), testConnection, ListDevicesInput{PageSize: 101})
	require.NoError(t, err)
	require.Equal(t, ListDevicesBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	return string(body)
}
