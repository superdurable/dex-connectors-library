// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dieyuyun

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "private-test-key"

var testConnection = sdkgo.ConnectionRef{Provider: "dieyuyun", Name: "dieyuyun"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "dieyuyun-provider-test" }
func (queryContext) StepExecutionID() string     { return "balance" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	client, err := New(Config{BaseURL: baseURL, MaxResponseBytes: maxResponseBytes}, credentials)
	require.NoError(t, err)
	return client
}
func TestAccountBalanceRequestAndExactDecimal(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/balance", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"object":"balance","currency":"CNY","balance":138.50,"api_key":{"id":"private","name":"private","daily_budget":20}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountBalanceBranchFetched, result.Branch)
	require.Equal(t, GetAccountBalanceOutput{AvailableCNY: "138.50"}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestAccountBalanceOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"unauthorized", 401, `{"error":{"code":"UNAUTHORIZED","message":"private-test-key"}}`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, `private`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"bad request", 400, `private`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limited", 429, `private`, "", sdkgo.FailureRateLimit, true},
		{"bad gateway", 502, `private`, "", sdkgo.FailureAvailability, true},
		{"malformed", 200, `{`, GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"wrong currency", 200, `{"object":"balance","currency":"USD","balance":1}`, GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing balance", 200, `{"object":"balance","currency":"CNY"}`, GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAPIKey)
			}
			if err != nil {
				require.NotContains(t, err.Error(), testAPIKey)
			}
		})
	}
}
func TestAccountBalanceSizeAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"object":"balance","currency":"CNY","balance":1.00}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountBalanceBranchInvalidResponse, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIKey)
}
