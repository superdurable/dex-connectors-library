// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cainiao

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "cainiao", Name: "cainiao"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "cainiao-provider-test" }
func (queryContext) StepExecutionID() string     { return "query-waybill-trace" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AppKey: "123456", AppSecret: sdkgo.NewSecretString("secretKey"), SessionKey: sdkgo.NewSecretString("sessionToken")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}
func TestTOPHMACMD5SignatureVector(t *testing.T) {
	// Independent expected value from the documented sorted-field HMAC-MD5 algorithm.
	fields := url.Values{"app_key": {"123456"}, "cp_code": {"YTO"}, "format": {"json"}, "method": {"cainiao.waybill.logisticstrace.get"}, "session": {"sessionToken"}, "sign_method": {"hmac"}, "timestamp": {"2026-10-22 15:26:40"}, "v": {"2.0"}, "waybill_code": {"12345"}}
	canonical := "app_key123456cp_codeYTOformatjsonmethodcainiao.waybill.logisticstrace.getsessionsessionTokensign_methodhmactimestamp2026-10-22 15:26:40v2.0waybill_code12345"
	signer := hmac.New(md5.New, []byte("secretKey"))
	_, err := signer.Write([]byte(canonical))
	require.NoError(t, err)
	require.Equal(t, strings.ToUpper(hex.EncodeToString(signer.Sum(nil))), signRequest("secretKey", fields))
}
func TestQueryWaybillTraceSignsRequestAndReadsEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "application/x-www-form-urlencoded; charset=utf-8", request.Header.Get("Content-Type"))
		require.NoError(t, request.ParseForm())
		fields := request.PostForm
		require.Equal(t, "123456", fields.Get("app_key"))
		require.Equal(t, "sessionToken", fields.Get("session"))
		require.Equal(t, "YTO", fields.Get("cp_code"))
		require.Equal(t, "12345", fields.Get("waybill_code"))
		require.Equal(t, "cainiao.waybill.logisticstrace.get", fields.Get("method"))
		require.Equal(t, "hmac", fields.Get("sign_method"))
		require.Equal(t, "json", fields.Get("format"))
		require.Equal(t, "2.0", fields.Get("v"))
		require.NotContains(t, request.PostForm.Encode(), "secretKey")
		require.Regexp(t, regexp.MustCompile(`^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d$`), fields.Get("timestamp"))
		timestamp, err := time.ParseInLocation("2006-01-02 15:04:05", fields.Get("timestamp"), time.FixedZone("GMT+8", 8*3600))
		require.NoError(t, err)
		require.WithinDuration(t, time.Now(), timestamp, 5*time.Second)
		require.Equal(t, signRequest("secretKey", fields), fields.Get("sign"))
		_, err = io.WriteString(writer, `{"cainiao_waybill_logisticstrace_get_response":{"logistics_detail_dto_list":{"logistics_detail_dto":[{"action":"GOT","description":"已揽收","time":"2016-07-14 17:00:42"}]}}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{CarrierCode: "YTO", WaybillCode: "12345"})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchFound, result.Branch)
	require.Equal(t, WaybillTrace{CarrierCode: "YTO", WaybillCode: "12345", Events: []WaybillEvent{{Action: "GOT", Description: "已揽收", Time: "2016-07-14 17:00:42"}}}, result.Value)
}
func TestQueryWaybillTraceClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"provider rejection", 200, `{"error_response":{"code":50,"msg":"Remote service error","sub_code":"isv.invalid-parameter"}}`, QueryWaybillTraceBranchProviderRejected, false},
		{"unauthorized", 401, `{}`, QueryWaybillTraceBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing list", 200, `{"cainiao_waybill_logisticstrace_get_response":{}}`, QueryWaybillTraceBranchInvalidResponse, false},
		{"missing event field", 200, `{"cainiao_waybill_logisticstrace_get_response":{"logistics_detail_dto_list":{"logistics_detail_dto":[{"action":"GOT"}]}}}`, QueryWaybillTraceBranchInvalidResponse, false},
		{"malformed", 200, `{"cainiao_waybill_logisticstrace_get_response":`, QueryWaybillTraceBranchInvalidResponse, false},
		{"trailing", 200, `{"cainiao_waybill_logisticstrace_get_response":{"logistics_detail_dto_list":{"logistics_detail_dto":[]}}} {}`, QueryWaybillTraceBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{CarrierCode: "YTO", WaybillCode: "12345"})
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
func TestQueryWaybillTraceValidatesInputAndBoundsResponse(t *testing.T) {
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchDefect, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{CarrierCode: "YTO", WaybillCode: "12345"})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchDefect, result.Branch)
	_, err = New(Config{BaseURL: "https://eco.taobao.com/router/rest?session=secret"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"cainiao_waybill_logisticstrace_get_response":{"logistics_detail_dto_list":{"logistics_detail_dto":[]}}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).QueryWaybillTrace(), testConnection, QueryWaybillTraceInput{CarrierCode: "YTO", WaybillCode: "12345"})
	require.NoError(t, err)
	require.Equal(t, QueryWaybillTraceBranchInvalidResponse, result.Branch)
}
