// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package yunda

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "yunda", Name: "yunda"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "yunda-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-waybill-balance" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{
		AppKey: sdkgo.NewSecretString("999999"), AppSecret: sdkgo.NewSecretString("app-secret"),
		PartnerID: sdkgo.NewSecretString("YUNDA"), BaimaSecret: sdkgo.NewSecretString("baima-secret"),
	}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}), WithClock(func() time.Time { return time.UnixMilli(1731510000000) }))
	require.NoError(t, err)
	return client
}

func TestWaybillBalanceSignatureFixedVector(t *testing.T) {
	body := []byte(`{"appid":"999999","partner_id":"YUNDA","secret":"baima-secret","type":"common"}`)
	require.Equal(t, "c90a0b67ac4949195a20fc22586d6f51", waybillBalanceSignature(body, "app-secret"))
}
func TestGetWaybillBalanceSignedRequestAndFilteredOutput(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, waybillBalanceEndpoint, request.URL.String())
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		require.Equal(t, "999999", request.Header.Get("app-key"))
		require.Equal(t, "1731510000000", request.Header.Get("req-time"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"appid":"999999","partner_id":"YUNDA","secret":"baima-secret","type":"common"}`, string(body))
		require.Equal(t, waybillBalanceSignature(body, "app-secret"), request.Header.Get("sign"))
		return reply(200, `{"code":"0000","result":true,"message":"secret","data":{"status":"1","msg":"secret","free_num":"1","remain_num":"2","total_num":"0","used_num":"0"}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetWaybillBalance(), testConnection, GetWaybillBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetWaybillBalanceBranchFound, result.Branch)
	require.Equal(t, GetWaybillBalanceOutput{FreeCount: 1, PaidCount: 2}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret")
}
func TestGetWaybillBalanceOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"rejected", 200, `{"code":"4001","result":false,"message":"baima-secret"}`, GetWaybillBalanceBranchProviderRejected, false},
		{"temporary", 200, `{"code":"7777","result":false}`, "", true},
		{"throttle", 429, `{}`, "", true},
		{"server", 503, `{}`, "", true},
		{"malformed", 200, `{"code":"0000"}`, GetWaybillBalanceBranchInvalidResponse, false},
		{"missing", 200, `{"code":"0000","result":true,"data":{"status":"1"}}`, GetWaybillBalanceBranchInvalidResponse, false},
		{"negative", 200, `{"code":"0000","result":true,"data":{"status":"1","free_num":"-1","remain_num":"2"}}`, GetWaybillBalanceBranchInvalidResponse, false},
		{"wrong status", 200, `{"code":"0000","result":true,"data":{"status":"0","free_num":"1","remain_num":"2"}}`, GetWaybillBalanceBranchInvalidResponse, false},
		{"redirect", 302, `{}`, GetWaybillBalanceBranchProviderRejected, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				response := reply(scenario.status, scenario.body)
				if scenario.status == 302 {
					response.Header.Set("Location", "https://other.example/steal")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetWaybillBalance(), testConnection, GetWaybillBalanceInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "secret")
			}
		})
	}
}
func TestGetWaybillBalanceTransportAndBounds(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("baima-secret") })
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetWaybillBalance(), testConnection, GetWaybillBalanceInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "baima-secret")
	large := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, large, 16).GetWaybillBalance(), testConnection, GetWaybillBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetWaybillBalanceBranchInvalidResponse, result.Branch)
}
