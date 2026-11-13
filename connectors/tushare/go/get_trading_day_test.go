// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tushare

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

var testConnection = sdkgo.ConnectionRef{Provider: "tushare", Name: "tushare"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "tushare-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-trading-day" }
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
	credentials := Credentials{Token: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetTradingDayRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "https://api.tushare.pro", request.URL.String())
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var body struct {
			APIName string            `json:"api_name"`
			Token   string            `json:"token"`
			Params  map[string]string `json:"params"`
			Fields  string            `json:"fields"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, "trade_cal", body.APIName)
		require.Equal(t, "test-secret", body.Token)
		require.Equal(t, map[string]string{"exchange": "SSE", "start_date": "20261113", "end_date": "20261113"}, body.Params)
		require.Equal(t, "exchange,cal_date,is_open,pretrade_date", body.Fields)
		return reply(200, `{"code":0,"data":{"fields":["exchange","cal_date","is_open","pretrade_date"],"items":[["SSE","20261113",1,"20261112"]]},"secret":"ignore"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTradingDay(), testConnection, GetTradingDayInput{Exchange: "SSE", Date: "20261113"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetTradingDayBranchFound, result.Branch)
	require.Equal(t, GetTradingDayOutput{Exchange: "SSE", Date: "20261113", IsOpen: true, PreviousTradingDate: "20261112"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "test-secret")
}
func TestGetTradingDayClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"no date", 200, `{"code":0,"data":{"fields":["exchange","cal_date","is_open","pretrade_date"],"items":[]}}`, GetTradingDayBranchNoData, false},
		{"permission", 200, `{"code":2002,"msg":"test-secret"}`, GetTradingDayBranchProviderRejected, false},
		{"rejection", 200, `{"code":1001,"msg":"test-secret"}`, GetTradingDayBranchProviderRejected, false},
		{"unauthorized", 401, `{}`, GetTradingDayBranchProviderRejected, false},
		{"rate", 429, `{}`, "", true},
		{"system", 503, `{}`, "", true},
		{"missing code", 200, `{}`, GetTradingDayBranchInvalidResponse, false},
		{"bad fields", 200, `{"code":0,"data":{"fields":["cal_date"],"items":[]}}`, GetTradingDayBranchInvalidResponse, false},
		{"bad row", 200, `{"code":0,"data":{"fields":["exchange","cal_date","is_open","pretrade_date"],"items":[["SZSE","20261113",1,"20261112"]]}}`, GetTradingDayBranchInvalidResponse, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTradingDay(), testConnection, GetTradingDayInput{Exchange: "SSE", Date: "20261113"})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "test-secret")
		})
	}
}
func TestGetTradingDayInvalidInputAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	client := newTestClient(t, transport, 0)
	for _, input := range []GetTradingDayInput{{Exchange: "NYSE", Date: "20261113"}, {Exchange: "SSE", Date: "20261301"}, {Exchange: "SSE", Date: "bad"}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetTradingDay(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, GetTradingDayBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetTradingDay(), testConnection, GetTradingDayInput{Exchange: "SSE", Date: "20261113"})
	require.NoError(t, err)
	require.Equal(t, GetTradingDayBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetTradingDay(), testConnection, GetTradingDayInput{Exchange: "SSE", Date: "20261113"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
