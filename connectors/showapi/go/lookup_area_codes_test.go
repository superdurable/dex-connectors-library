// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package showapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "showapi", Name: "showapi"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "showapi-provider-test" }
func (queryContext) StepExecutionID() string     { return "lookup-area-codes" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, url string, key string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: url}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString(key)}})
	require.NoError(t, err)
	return client
}
func TestLookupAreaCodesRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/9-3", r.URL.Path)
		require.Equal(t, "secretKey", r.URL.Query().Get("appKey"))
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "area=%E4%B8%BD%E6%B1%9F", string(body))
		_, err = w.Write([]byte(`{"showapi_res_code":0,"showapi_res_body":{"ret_code":0,"list":[{"prov":"云南","area":"丽江","areaCode":"530700","cityInfo":{"latitude":26.9}}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, "secretKey").LookupAreaCodes(), testConnection, LookupAreaCodesInput{Area: "丽江"})
	require.NoError(t, err)
	require.Equal(t, LookupAreaCodesBranchFound, result.Branch)
	require.Equal(t, AreaCodeMatches{Matches: []AreaCodeMatch{{Province: "云南", Area: "丽江", AreaCode: "530700"}}}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestLookupAreaCodesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"showapi_res_code":0,"showapi_res_body":{"ret_code":0,"list":[]}}`, LookupAreaCodesBranchFound, false},
		{"invalid key", 200, `{"showapi_res_code":-1004,"showapi_res_body":{}}`, LookupAreaCodesBranchProviderRejected, false},
		{"business rejection", 200, `{"showapi_res_code":0,"showapi_res_body":{"ret_code":3}}`, LookupAreaCodesBranchProviderRejected, false},
		{"forbidden", 403, `{}`, LookupAreaCodesBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, LookupAreaCodesBranchInvalidResponse, false},
		{"missing list", 200, `{"showapi_res_code":0,"showapi_res_body":{"ret_code":0}}`, LookupAreaCodesBranchInvalidResponse, false},
		{"invalid area", 200, `{"showapi_res_code":0,"showapi_res_body":{"ret_code":0,"list":[{"area":"丽江"}]}}`, LookupAreaCodesBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, "secretKey").LookupAreaCodes(), testConnection, LookupAreaCodesInput{Area: "丽江"})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidInputAndKeyNeverCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	for _, tc := range []struct {
		input LookupAreaCodesInput
		key   string
	}{{LookupAreaCodesInput{}, "secretKey"}, {LookupAreaCodesInput{Area: "丽江"}, "key with space"}} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, tc.key).LookupAreaCodes(), testConnection, tc.input)
		require.NoError(t, err)
		require.Equal(t, LookupAreaCodesBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}

func TestTransportErrorDoesNotExposeAppKey(t *testing.T) {
	client := newTestClient(t, "http://127.0.0.1:1", "privateKey")
	_, err := sdkgo.RunQuery(queryContext{}, client.LookupAreaCodes(), testConnection, LookupAreaCodesInput{Area: "丽江"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "privateKey")
	require.NotContains(t, err.Error(), "appKey=")
}
