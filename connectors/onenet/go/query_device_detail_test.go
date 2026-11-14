// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onenet

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "onenet", Name: "onenet"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "onenet-provider-test" }
func (queryContext) StepExecutionID() string     { return "query-device-detail" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{UserID: "130037", AccessKey: sdkgo.NewSecretString(base64.StdEncoding.EncodeToString([]byte("test-access-key"))), ProductID: "demo123"}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}
func TestQueryDeviceDetailSignsRequestAndDropsSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/device/detail", request.URL.Path)
		require.Equal(t, "demo123", request.URL.Query().Get("product_id"))
		require.Equal(t, "传感器 1", request.URL.Query().Get("device_name"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		authorization := request.Header.Get("Authorization")
		parts, err := url.ParseQuery(authorization)
		require.NoError(t, err)
		require.Equal(t, "2022-05-01", parts.Get("version"))
		require.Equal(t, "userid/130037", parts.Get("res"))
		require.Equal(t, "sha256", parts.Get("method"))
		expiresAt, err := strconv.ParseInt(parts.Get("et"), 10, 64)
		require.NoError(t, err)
		require.WithinDuration(t, time.Now().Add(time.Hour), time.Unix(expiresAt, 0), 3*time.Second)
		digest := hmac.New(sha256.New, []byte("test-access-key"))
		_, err = digest.Write([]byte(parts.Get("et") + "\nsha256\nuserid/130037\n2022-05-01"))
		require.NoError(t, err)
		require.Equal(t, base64.StdEncoding.EncodeToString(digest.Sum(nil)), parts.Get("sign"))
		_, err = io.WriteString(writer, `{"code":0,"data":{"pid":"demo123","name":"传感器 1","status":1,"desc":"sensor","create_time":"2025-11-28T09:49:00+08:00","last_time":"2025-12-01T15:38:51+08:00","sec_key":"DO-NOT-EXPOSE","psk":"DO-NOT-EXPOSE"},"msg":"succ"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).QueryDeviceDetail(), testConnection, QueryDeviceDetailInput{DeviceName: "传感器 1"})
	require.NoError(t, err)
	require.Equal(t, QueryDeviceDetailBranchFound, result.Branch)
	require.Equal(t, "传感器 1", result.Value.DeviceName)
	require.Equal(t, 1, result.Value.Status)
	require.NotContains(t, strings.Join([]string{result.Value.ProductID, result.Value.DeviceName, result.Value.Description, result.Value.CreateTime, result.Value.LastOnlineTime}, " "), "DO-NOT-EXPOSE")
}
func TestQueryDeviceDetailClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"not found", 200, `{"code":1001,"msg":"not found"}`, QueryDeviceDetailBranchProviderRejected, false},
		{"unauthorized", 401, `{}`, QueryDeviceDetailBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing code", 200, `{"data":{}}`, QueryDeviceDetailBranchInvalidResponse, false},
		{"missing detail", 200, `{"code":0}`, QueryDeviceDetailBranchInvalidResponse, false},
		{"bad status", 200, `{"code":0,"data":{"pid":"p","name":"d","status":7}}`, QueryDeviceDetailBranchInvalidResponse, false},
		{"malformed", 200, `{"code":`, QueryDeviceDetailBranchInvalidResponse, false},
		{"trailing", 200, `{"code":0,"data":{"pid":"p","name":"d","status":1}} {}`, QueryDeviceDetailBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).QueryDeviceDetail(), testConnection, QueryDeviceDetailInput{DeviceName: "sensor-1"})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestQueryDeviceDetailBoundsResponseAndValidatesInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"code":0,"data":{"pid":"p","name":"d","status":1}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).QueryDeviceDetail(), testConnection, QueryDeviceDetailInput{DeviceName: "sensor-1"})
	require.NoError(t, err)
	require.Equal(t, QueryDeviceDetailBranchInvalidResponse, result.Branch)
	require.False(t, validDeviceName("\n"))
	require.False(t, validResourceID("../unsafe"))
	require.False(t, validDecimalID("a1"))
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, client.QueryDeviceDetail(), testConnection, QueryDeviceDetailInput{DeviceName: "sensor-1"})
	require.NoError(t, err)
	require.Equal(t, QueryDeviceDetailBranchDefect, result.Branch)
}
