// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jdlogistics

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "jdlogistics", Name: "jdlogistics"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "jdlogistics-provider-test" }
func (queryContext) StepExecutionID() string     { return "query-waybill-trace" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AppKey: "app123", AppSecret: sdkgo.NewSecretString("secretKey"), AccessToken: sdkgo.NewSecretString("token123"), CustomerCode: "010K123"}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	client.clock = func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 0, time.FixedZone("CST", 8*3600)) }
	return client
}
func TestSignatureVector(t *testing.T) {
	content := "secretKeyaccess_tokentoken123app_keyapp123method/ecap/v1/orders/trace/queryparam_json[{\"waybillCode\":\"JDX009866825381\",\"orderOrigin\":1,\"customerCode\":\"010K123\"}]timestamp2025-01-02 03:04:05v2.0secretKey"
	sum := md5.Sum([]byte(content))
	require.Equal(t, "7B4FF493E60C543840164F97AC341AD2", strings.ToUpper(hex.EncodeToString(sum[:])))
}
func TestQueryWaybillTraceSignsAndMinimizesOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, queryPath, request.URL.Path)
		require.Equal(t, "application/json;charset=UTF-8", request.Header.Get("Content-Type"))
		parameters := request.URL.Query()
		require.Equal(t, "app123", parameters.Get("app_key"))
		require.Equal(t, "token123", parameters.Get("access_token"))
		require.Equal(t, "ECAP", parameters.Get("LOP-DN"))
		require.Equal(t, "2025-01-02 03:04:05", parameters.Get("timestamp"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `[{"waybillCode":"JDX009866825381","orderOrigin":1,"customerCode":"010K123"}]`, string(body))
		signed := "secretKeyaccess_tokentoken123app_keyapp123method" + queryPath + "param_json" + string(body) + "timestamp2025-01-02 03:04:05v2.0secretKey"
		sum := md5.Sum([]byte(signed))
		require.Equal(t, strings.ToUpper(hex.EncodeToString(sum[:])), parameters.Get("sign"))
		require.NotContains(t, string(body), "secretKey")
		_, err = io.WriteString(writer, `{"success":true,"code":0,"data":{"traceDetails":[{"waybillCode":"JDX009866825381","state":"200001","categoryName":"已揽收","operationTime":"2022-04-07 20:22:50","operatorName":"张三","operationRemark":"电话 13100000001"}]}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{WaybillNo: "JDX009866825381"})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchFound, result.Branch)
	require.Equal(t, WaybillTrace{WaybillNo: "JDX009866825381", Events: []WaybillEvent{{State: "200001", CategoryName: "已揽收", OperationTime: "2022-04-07 20:22:50"}}}, result.Value)
}
func TestQueryWaybillTraceClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{{"rejected", 200, `{"success":false,"code":2001}`, QueryWaybillTraceBranchProviderRejected, false}, {"service error", 200, `{"success":false,"code":4000}`, "", true}, {"unauthorized", 401, `{}`, QueryWaybillTraceBranchProviderRejected, false}, {"rate limited", 429, `{}`, "", true}, {"unavailable", 503, `{}`, "", true}, {"missing data", 200, `{"success":true,"code":0}`, QueryWaybillTraceBranchInvalidResponse, false}, {"different bill", 200, `{"success":true,"code":0,"data":{"traceDetails":[{"waybillCode":"other","state":"200001","operationTime":"2022-01-01 01:01:01"}]}}`, QueryWaybillTraceBranchInvalidResponse, false}, {"missing scan", 200, `{"success":true,"code":0,"data":{"traceDetails":[{"state":"200001"}]}}`, QueryWaybillTraceBranchInvalidResponse, false}, {"malformed", 200, `{"success":`, QueryWaybillTraceBranchInvalidResponse, false}, {"trailing", 200, `{"success":true,"code":0,"data":{}} {}`, QueryWaybillTraceBranchInvalidResponse, false}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{WaybillNo: "JDX009866825381"})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, err.Error(), "secretKey")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestQueryWaybillTraceTransportCredentialsAndBounds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, endpoint, 0).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{WaybillNo: "JDX009866825381"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchDefect, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{WaybillNo: "JDX009866825381"})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchDefect, result.Branch)
	_, err = New(Config{BaseURL: "https://api.jdl.com/evil?secret=x"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"success":true,"code":0,"data":{"traceDetails":[]}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{WaybillNo: "JDX009866825381"})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchInvalidResponse, result.Branch)
}
