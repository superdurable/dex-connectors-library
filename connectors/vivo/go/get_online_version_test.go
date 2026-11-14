// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vivo

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

var testConnection = sdkgo.ConnectionRef{Provider: "vivo", Name: "vivo"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "vivo-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-online-version" }
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
	credentials := Credentials{Account: sdkgo.NewSecretString("test-account"), Secret: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}), WithClock(func() time.Time { return time.UnixMilli(1731510000000) }))
	require.NoError(t, err)
	return client
}

var packageInput = GetOnlineVersionInput{PackageName: "com.example.app"}

func TestOnlineVersionSignatureFixedVector(t *testing.T) {
	require.Equal(t, "2e2fb41bf50cef85f5a7b6192ad9c4bcbe1cb5bc9dc9ddedaa851637d01a6da8", onlineVersionSignature("test-account", "com.example.app", 1731510000000, "test-secret"))
}
func TestGetOnlineVersionSignedRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, onlineVersionEndpoint, request.URL.String())
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var sent struct {
			Account   string `json:"account"`
			Timestamp int64  `json:"timestamp"`
			Sign      string `json:"sign"`
			BizParam  struct {
				PackageName string `json:"packageName"`
			} `json:"bizParam"`
		}
		require.NoError(t, json.Unmarshal(body, &sent))
		require.Equal(t, "test-account", sent.Account)
		require.Equal(t, int64(1731510000000), sent.Timestamp)
		require.Equal(t, "com.example.app", sent.BizParam.PackageName)
		require.Equal(t, "2e2fb41bf50cef85f5a7b6192ad9c4bcbe1cb5bc9dc9ddedaa851637d01a6da8", sent.Sign)
		return reply(200, `{"code":0,"msg":"success","data":{"packageName":"com.example.app","versionName":"3.2.1","versionCode":"100","private":"test-secret"}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetOnlineVersion(), testConnection, packageInput)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetOnlineVersionBranchFound, result.Branch)
	require.Equal(t, GetOnlineVersionOutput{PackageName: "com.example.app", VersionName: "3.2.1", VersionCode: 100}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "test-secret")
}
func TestGetOnlineVersionOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"numeric code", 200, `{"code":0,"data":{"packageName":"com.example.app","versionName":"4.0","versionCode":101}}`, GetOnlineVersionBranchFound, false},
		{"no app", 200, `{"code":11001}`, GetOnlineVersionBranchNoData, false},
		{"bad sign", 200, `{"code":10001,"msg":"test-secret"}`, GetOnlineVersionBranchProviderRejected, false},
		{"provider throttled", 200, `{"code":10006}`, "", true},
		{"http throttled", 429, `{}`, "", true},
		{"server", 503, `{}`, "", true},
		{"missing data", 200, `{"code":0}`, GetOnlineVersionBranchInvalidResponse, false},
		{"mismatch", 200, `{"code":0,"data":{"packageName":"com.other.app","versionName":"1","versionCode":1}}`, GetOnlineVersionBranchInvalidResponse, false},
		{"redirect", 302, `{}`, GetOnlineVersionBranchProviderRejected, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				response := reply(scenario.status, scenario.body)
				if scenario.status == 302 {
					response.Header.Set("Location", "https://other.example/steal")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetOnlineVersion(), testConnection, packageInput)
			require.Equal(t, 1, calls)
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "test-secret")
			}
		})
	}
}
func TestGetOnlineVersionValidationAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("test-secret") })
	client := newTestClient(t, transport, 0)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetOnlineVersion(), testConnection, GetOnlineVersionInput{PackageName: "bad package"})
	require.NoError(t, err)
	require.Equal(t, GetOnlineVersionBranchDefect, result.Branch)
	require.Zero(t, calls)
	_, err = sdkgo.RunQuery(queryContext{}, client.GetOnlineVersion(), testConnection, packageInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
func TestGetOnlineVersionBoundsResponse(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetOnlineVersion(), testConnection, packageInput)
	require.NoError(t, err)
	require.Equal(t, GetOnlineVersionBranchInvalidResponse, result.Branch)
}
