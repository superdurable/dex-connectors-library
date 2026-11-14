// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dada

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "dada", Name: "dada"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "dada-provider-test" }
func (queryContext) StepExecutionID() string     { return "query-order-status" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AppKey: "123456", AppSecret: sdkgo.NewSecretString("abcdefg123"), SourceID: "73753"}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}
func TestOfficialSignatureVector(t *testing.T) {
	require.Equal(t, "71CEE2D6544277940D8C72979E71D0E1", signRequest("abcdefg123", "123456", `{"order_id":"20170301000001"}`, "73753", "1488363493"))
}
func TestQueryOrderStatusSignsOfficialRequestAndReadsStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/order/status/query", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "application/json; charset=utf-8", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var envelope struct {
			AppKey    string `json:"app_key"`
			AppSecret string `json:"app_secret"`
			Body      string `json:"body"`
			Format    string `json:"format"`
			Signature string `json:"signature"`
			SourceID  string `json:"source_id"`
			Timestamp string `json:"timestamp"`
			Version   string `json:"v"`
		}
		require.NoError(t, json.Unmarshal(body, &envelope))
		require.Empty(t, envelope.AppSecret)
		require.Equal(t, "123456", envelope.AppKey)
		require.Equal(t, `{"order_id":"orderNo1653708880866"}`, envelope.Body)
		require.Equal(t, "json", envelope.Format)
		require.Equal(t, "1.0", envelope.Version)
		require.Equal(t, "73753", envelope.SourceID)
		timestamp, err := strconv.ParseInt(envelope.Timestamp, 10, 64)
		require.NoError(t, err)
		require.WithinDuration(t, time.Now(), time.Unix(timestamp, 0), 5*time.Second)
		require.Equal(t, signRequest("abcdefg123", envelope.AppKey, envelope.Body, envelope.SourceID, envelope.Timestamp), envelope.Signature)
		_, err = io.WriteString(writer, `{"status":"success","code":0,"result":{"orderId":"orderNo1653708880866","statusCode":1,"statusMsg":"待接单","transporterPhone":"18500000000"}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).QueryOrderStatus(), testConnection, QueryOrderStatusInput{OrderID: "orderNo1653708880866"})
	require.NoError(t, err)
	require.Equal(t, QueryOrderStatusBranchFound, result.Branch)
	require.Equal(t, OrderStatus{OrderID: "orderNo1653708880866", StatusCode: 1, StatusMessage: "待接单"}, result.Value)
}
func TestQueryOrderStatusClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"provider rejection", 200, `{"status":"fail","code":2070,"msg":"rejected"}`, QueryOrderStatusBranchProviderRejected, false},
		{"unauthorized", 401, `{}`, QueryOrderStatusBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing result", 200, `{"status":"success","code":0}`, QueryOrderStatusBranchInvalidResponse, false},
		{"wrong order", 200, `{"status":"success","code":0,"result":{"orderId":"other","statusCode":1,"statusMsg":"待接单"}}`, QueryOrderStatusBranchInvalidResponse, false},
		{"missing status code", 200, `{"status":"success","code":0,"result":{"orderId":"orderNo1653708880866","statusMsg":"待接单"}}`, QueryOrderStatusBranchInvalidResponse, false},
		{"malformed", 200, `{"status":`, QueryOrderStatusBranchInvalidResponse, false},
		{"trailing", 200, `{"status":"success","code":0,"result":{"orderId":"orderNo1653708880866","statusCode":1,"statusMsg":"待接单"}} {}`, QueryOrderStatusBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).QueryOrderStatus(), testConnection, QueryOrderStatusInput{OrderID: "orderNo1653708880866"})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, err.Error(), "abcdefg123")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestQueryOrderStatusValidatesInputsAndBoundsResponse(t *testing.T) {
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.QueryOrderStatus(), testConnection, QueryOrderStatusInput{OrderID: ""})
	require.NoError(t, err)
	require.Equal(t, QueryOrderStatusBranchDefect, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.QueryOrderStatus(), testConnection, QueryOrderStatusInput{OrderID: "valid"})
	require.NoError(t, err)
	require.Equal(t, QueryOrderStatusBranchDefect, result.Branch)
	_, err = New(Config{BaseURL: "https://newopen.imdada.cn/api"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"status":"success","code":0,"result":{"orderId":"orderNo1653708880866","statusCode":1,"statusMsg":"待接单"}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).QueryOrderStatus(), testConnection, QueryOrderStatusInput{OrderID: "orderNo1653708880866"})
	require.NoError(t, err)
	require.Equal(t, QueryOrderStatusBranchInvalidResponse, result.Branch)
}
