// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zto

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

var testConnection = sdkgo.ConnectionRef{Provider: "zto", Name: "zto"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "zto-provider-test" }
func (queryContext) StepExecutionID() string     { return "query-waybill-trace" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AppKey: "app123", AppSecret: sdkgo.NewSecretString("secretKey")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}
func TestStandardDigestVector(t *testing.T) {
	body := []byte(`{"billCode":"73100059800035"}`)
	digest := md5.Sum(append(body, []byte("secretKey")...))
	require.Equal(t, "TzhKEP6EgGeAhZkmosT2Mw==", base64.StdEncoding.EncodeToString(digest[:]))
}
func TestQueryWaybillTraceSignsRequestAndRedactsPersonalFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/zto.merchant.waybill.track.query", request.URL.Path)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		require.Equal(t, "app123", request.Header.Get("x-appKey"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"billCode":"73100059800035"}`, string(body))
		require.NotContains(t, string(body), "secretKey")
		require.NotContains(t, string(body), "mobilePhone")
		sum := md5.Sum(append(body, []byte("secretKey")...))
		require.Equal(t, base64.StdEncoding.EncodeToString(sum[:]), request.Header.Get("x-dataDigest"))
		_, err = io.WriteString(writer, `{"status":true,"statusCode":"SYS000","message":"请求成功","result":[{"billCode":"73100059800035","scanType":"收件","scanDate":1676429769000,"scanSite":{"name":"金华浦江县","phone":"0579-82405676"},"signMan":"李四","operateUserPhone":"13386970965","desc":"请电联 13860810307"}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{BillCode: "73100059800035"})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchFound, result.Branch)
	require.Equal(t, WaybillTrace{BillCode: "73100059800035", Events: []WaybillEvent{{ScanType: "收件", ScanDate: 1676429769000, ScanSiteName: "金华浦江县"}}}, result.Value)
}
func TestQueryWaybillTraceClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"invalid parameter", 200, `{"status":false,"statusCode":"E403","message":"参数校验失败","result":null}`, QueryWaybillTraceBranchProviderRejected, false},
		{"provider rejection", 200, `{"status":false,"statusCode":"E404","message":"未绑定电子面单账号","result":null}`, QueryWaybillTraceBranchProviderRejected, false},
		{"phone mismatch", 200, `{"status":false,"statusCode":"E409","message":"电话校验不一致","result":null}`, QueryWaybillTraceBranchProviderRejected, false},
		{"phone digits required", 200, `{"status":false,"statusCode":"E413","message":"请输入电话后四位","result":null}`, QueryWaybillTraceBranchProviderRejected, false},
		{"system error", 200, `{"status":false,"statusCode":"E500","message":"异常","result":null}`, "", true},
		{"unauthorized", 401, `{}`, QueryWaybillTraceBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing result", 200, `{"status":true,"statusCode":"SYS000"}`, QueryWaybillTraceBranchInvalidResponse, false},
		{"different bill", 200, `{"status":true,"statusCode":"SYS000","result":[{"billCode":"wrong","scanType":"收件","scanDate":1676429769000}]}`, QueryWaybillTraceBranchInvalidResponse, false},
		{"missing scan", 200, `{"status":true,"statusCode":"SYS000","result":[{"billCode":"73100059800035"}]}`, QueryWaybillTraceBranchInvalidResponse, false},
		{"malformed", 200, `{"status":`, QueryWaybillTraceBranchInvalidResponse, false},
		{"trailing", 200, `{"status":true,"statusCode":"SYS000","result":[]} {}`, QueryWaybillTraceBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{BillCode: "73100059800035"})
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
func TestQueryWaybillTraceValidatesInputCredentialsAndBounds(t *testing.T) {
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchDefect, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{BillCode: "73100059800035"})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchDefect, result.Branch)
	_, err = New(Config{BaseURL: "https://japi.zto.com/evil?secret=x"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"status":true,"statusCode":"SYS000","result":[]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{BillCode: "73100059800035"})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchInvalidResponse, result.Branch)
	require.False(t, strings.Contains(result.Failure.Message, "secretKey"))
}
