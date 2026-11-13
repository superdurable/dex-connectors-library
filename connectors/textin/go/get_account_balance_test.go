// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textin

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

const testSecretCode = "private-test-secret"
const testAppID = "test-app-id"

var testConnection = sdkgo.ConnectionRef{Provider: "textin", Name: "textin"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "textin-provider-test" }
func (queryContext) StepExecutionID() string     { return "account-balance" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: testAppID, SecretCode: sdkgo.NewSecretString(testSecretCode)}}
	client, err := New(Config{BaseURL: baseURL, MaxResponseBytes: maxResponseBytes}, credentials)
	require.NoError(t, err)
	return client
}
func TestAccountBalanceRequestAndExactDecimal(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/user/finance/balance", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, testAppID, request.Header.Get("x-ti-app-id"))
		require.Equal(t, testSecretCode, request.Header.Get("x-ti-secret-code"))
		require.Empty(t, request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"code":200,"msg":"success","data":{"balance":100,"available_balance":83.712,"pay_type":1,"postpaid_amount":100,"unavailable_balance":2,"unpaid":10,"private":"omitted"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountBalanceBranchFetched, result.Branch)
	require.Equal(t, GetAccountBalanceOutput{AvailableTCoins: "83.712", PayType: 1}, result.Value)
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
		{"bad credentials", 200, `{"code":40102,"message":"private-test-secret"}`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"missing credentials", 200, `{"code":40101}`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"bad parameter", 200, `{"code":40004}`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"provider unavailable", 200, `{"code":500}`, "", sdkgo.FailureAvailability, true},
		{"unauthorized", 401, `private`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"rate limit", 429, `private`, "", sdkgo.FailureRateLimit, true},
		{"service unavailable", 503, `private`, "", sdkgo.FailureAvailability, true},
		{"malformed", 200, `{`, GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing code", 200, `{"data":{}}`, GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing balance", 200, `{"code":200,"data":{"pay_type":1}}`, GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"invalid pay type", 200, `{"code":200,"data":{"available_balance":1.20,"pay_type":3}}`, GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
				if tc.status == 200 {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, err, &retry)
					require.Equal(t, tc.kind, retry.Failure.Kind)
				} else {
					var retry *dex.RetryAfterError
					require.ErrorAs(t, err, &retry)
					require.Equal(t, 2*time.Second, retry.After)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
			}
			if err != nil {
				require.NotContains(t, err.Error(), testSecretCode)
			}
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, testSecretCode)
			}
		})
	}
}
func TestAccountBalanceSizeAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":200,"data":{"available_balance":1.00,"pay_type":1}}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountBalanceBranchInvalidResponse, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testSecretCode)
}
