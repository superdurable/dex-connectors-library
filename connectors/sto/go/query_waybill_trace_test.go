// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sto

import (
	"context"
	"crypto/md5"
	"encoding/base64"
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

var testConnection = sdkgo.ConnectionRef{Provider: "sto", Name: "sto"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "sto-provider-test" }
func (queryContext) StepExecutionID() string     { return "query-waybill-trace" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AppKey: "app123", AppSecret: sdkgo.NewSecretString("secretKey"), FromCode: "resource123"}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}
func TestOfficialSignatureVector(t *testing.T) {
	sum := md5.Sum([]byte(`{"orderNo":"8885452262"}123abc`))
	require.Equal(t, "PUHKHeU5C/FmWDbBCFpF/A==", base64.StdEncoding.EncodeToString(sum[:]))
}
func TestQueryWaybillTraceSignsFormAndMinimizesOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/gateway/link.do", request.URL.Path)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		require.NoError(t, request.ParseForm())
		fields := request.PostForm
		require.Equal(t, "STO_TRACE_QUERY_COMMON", fields.Get("api_name"))
		require.Equal(t, "app123", fields.Get("from_appkey"))
		require.Equal(t, "resource123", fields.Get("from_code"))
		require.Equal(t, "sto_trace_query", fields.Get("to_appkey"))
		require.Equal(t, "sto_trace_query", fields.Get("to_code"))
		require.JSONEq(t, `{"waybillNoList":["884000400421364"]}`, fields.Get("content"))
		require.NotContains(t, fields.Encode(), "secretKey")
		sum := md5.Sum([]byte(fields.Get("content") + "secretKey"))
		require.Equal(t, base64.StdEncoding.EncodeToString(sum[:]), fields.Get("data_digest"))
		_, err := io.WriteString(writer, `{"success":true,"errorCode":null,"errorMsg":null,"data":{"884000400421364":[{"waybillNo":"884000400421364","scanType":"收件","opTime":"2020-02-18 17:20:08","opOrgName":"青浦重固网点","opOrgTel":"021-2871662","opEmpName":"张三","bizEmpPhone":"13838885038","authCode":"435628","memo":"请电联 13838885038"}]}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/gateway/link.do", 0).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{WaybillNo: "884000400421364"})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchFound, result.Branch)
	require.Equal(t, WaybillTrace{WaybillNo: "884000400421364", Events: []WaybillEvent{{ScanType: "收件", OpTime: "2020-02-18 17:20:08", OpOrgName: "青浦重固网点"}}}, result.Value)
}
func TestQueryWaybillTraceClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"provider rejection", 200, `{"success":false,"errorCode":"S02","errorMsg":"签名失败"}`, QueryWaybillTraceBranchProviderRejected, false},
		{"unsubscribed", 200, `{"success":false,"errorCode":"S18","errorMsg":"未订阅"}`, QueryWaybillTraceBranchProviderRejected, false},
		{"retry hint", 200, `{"success":false,"errorCode":"temporary","needRetry":true}`, "", true},
		{"gateway unavailable", 200, `{"success":false,"errorCode":"S07"}`, "", true},
		{"unauthorized", 401, `{}`, QueryWaybillTraceBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing data", 200, `{"success":true}`, QueryWaybillTraceBranchInvalidResponse, false},
		{"different bill", 200, `{"success":true,"data":{"wrong":[]}}`, QueryWaybillTraceBranchInvalidResponse, false},
		{"missing scan", 200, `{"success":true,"data":{"884000400421364":[{"scanType":"收件"}]}}`, QueryWaybillTraceBranchInvalidResponse, false},
		{"malformed", 200, `{"success":`, QueryWaybillTraceBranchInvalidResponse, false},
		{"trailing", 200, `{"success":true,"data":{}} {}`, QueryWaybillTraceBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/gateway/link.do", 0).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{WaybillNo: "884000400421364"})
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
func TestQueryWaybillTraceRetriesTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL + "/gateway/link.do"
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, endpoint, 0).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{WaybillNo: "884000400421364"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), "secretKey")
}

func TestQueryWaybillTraceValidatesInputCredentialsAndBounds(t *testing.T) {
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchDefect, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{WaybillNo: "884000400421364"})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchDefect, result.Branch)
	_, err = New(Config{BaseURL: "https://cloudinter-linkgateway.sto.cn/evil?secret=x"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"success":true,"data":{}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/gateway/link.do", 5).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{WaybillNo: "884000400421364"})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchInvalidResponse, result.Branch)
	require.False(t, strings.Contains(result.Failure.Message, "secretKey"))
}
