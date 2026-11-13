// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oppo

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "oppo", Name: "oppo"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "oppo-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-push-quota" }
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
	credentials := Credentials{AppKey: sdkgo.NewSecretString("test-app-key"), MasterSecret: sdkgo.NewSecretString("test-master-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}), WithClock(func() time.Time { return time.UnixMilli(1731510000000) }))
	require.NoError(t, err)
	return client
}

func TestPushAuthSignatureFixedVector(t *testing.T) {
	require.Equal(t, "1338dbc313aa33e15ba3502b3063fb57efb796bf9625c537051484cdd5b94c0d", pushAuthSignature("test-app-key", "1731510000000", "test-master-secret"))
}
func TestGetPushQuotaExchangeAndFilteredOutput(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			require.Equal(t, authEndpoint, request.URL.String())
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			values, err := url.ParseQuery(string(body))
			require.NoError(t, err)
			require.Equal(t, "test-app-key", values.Get("app_key"))
			require.Equal(t, "1731510000000", values.Get("timestamp"))
			require.Equal(t, pushAuthSignature("test-app-key", "1731510000000", "test-master-secret"), values.Get("sign"))
			return reply(200, `{"code":0,"data":{"auth_token":"secret-token","create_time":"1731510000000"}}`), nil
		}
		require.Equal(t, pushQuotaEndpoint, request.URL.String())
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "secret-token", request.Header.Get("auth_token"))
		require.Empty(t, request.URL.RawQuery)
		return reply(200, `{"code":0,"data":{"total_count":100000,"push_count":10000,"remain_count":90000,"secret":"do-not-return"}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetPushQuota(), testConnection, GetPushQuotaInput{})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Equal(t, GetPushQuotaBranchFound, result.Branch)
	require.Equal(t, GetPushQuotaOutput{TotalCount: 100000, PushCount: 10000, RemainCount: 90000}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret")
}
func TestGetPushQuotaOutcomes(t *testing.T) {
	cases := []struct {
		name, stage string
		status      int
		body        string
		branch      sdkgo.BranchID
		retry       bool
	}{
		{"auth rejected", "auth", 200, `{"code":16,"message":"test-master-secret"}`, GetPushQuotaBranchProviderRejected, false},
		{"auth throttle", "auth", 200, `{"code":13}`, "", true},
		{"auth malformed", "auth", 200, `{"data":{}}`, GetPushQuotaBranchInvalidResponse, false},
		{"auth token missing", "auth", 200, `{"code":0,"data":{}}`, GetPushQuotaBranchInvalidResponse, false},
		{"auth redirect", "auth", 302, `{}`, GetPushQuotaBranchProviderRejected, false},
		{"quota rejected", "quota", 200, `{"code":11,"message":"secret-token"}`, GetPushQuotaBranchProviderRejected, false},
		{"quota throttle", "quota", 429, `{}`, "", true},
		{"quota unavailable", "quota", 503, `{}`, "", true},
		{"quota missing counters", "quota", 200, `{"code":0,"data":{}}`, GetPushQuotaBranchInvalidResponse, false},
		{"quota inconsistent", "quota", 200, `{"code":0,"data":{"total_count":1,"push_count":1,"remain_count":1}}`, GetPushQuotaBranchInvalidResponse, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if scenario.stage == "quota" && calls == 1 {
					return reply(200, `{"code":0,"data":{"auth_token":"secret-token"}}`), nil
				}
				response := reply(scenario.status, scenario.body)
				if scenario.status == 302 {
					response.Header.Set("Location", "https://other.example/steal")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetPushQuota(), testConnection, GetPushQuotaInput{})
			if scenario.stage == "auth" {
				require.Equal(t, 1, calls)
			} else {
				require.Equal(t, 2, calls)
			}
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
func TestGetPushQuotaTransportAndBounds(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-master-secret") })
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetPushQuota(), testConnection, GetPushQuotaInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-master-secret")
	large := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, large, 16).GetPushQuota(), testConnection, GetPushQuotaInput{})
	require.NoError(t, err)
	require.Equal(t, GetPushQuotaBranchInvalidResponse, result.Branch)
}
