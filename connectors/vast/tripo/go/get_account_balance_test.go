// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tripo

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const testAPIKey = "test-tripo-api-key"

var testConnection = sdkgo.ConnectionRef{Provider: "tripo", Name: "tripo"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "tripo-provider-test" }
func (queryContext) StepExecutionID() string     { return "account-balance" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	return client
}
func TestAccountBalanceRequestAndExactDecimals(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v3/account/balance", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"code":0,"data":{"balance":10000.00,"frozen":200.00,"private":"omitted"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountBalanceBranchFetched, result.Branch)
	require.Equal(t, GetAccountBalanceOutput{AvailableCredits: "10000.00", FrozenCredits: "200.00"}, result.Value)
	require.NotContains(t, result.Value.AvailableCredits, "private")
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
		{"bad key", 401, `{"message":"private"}`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, `{"message":"private"}`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"provider code", 200, `{"code":1004,"message":"private"}`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"provider auth", 200, `{"code":1002,"message":"private"}`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"provider quota", 200, `{"code":2010,"message":"private"}`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureQuotaExhausted, false},
		{"provider rate", 200, `{"code":1007,"message":"private"}`, "", sdkgo.FailureRateLimit, true},
		{"provider outage", 200, `{"code":1000,"message":"private"}`, "", sdkgo.FailureAvailability, true},
		{"rate limit", 429, `private response`, "", sdkgo.FailureRateLimit, true},
		{"unavailable", 503, `private response`, "", sdkgo.FailureAvailability, true},
		{"bad JSON", 200, `{`, GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing code", 200, `{"data":{}}`, GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing balance", 200, `{"code":0,"data":{}}`, GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"invalid balance", 200, `{"code":0,"data":{"balance":-1,"frozen":3}}`, GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
			if tc.retry {
				if tc.status == http.StatusOK {
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
				require.NotContains(t, err.Error(), testAPIKey)
				require.NotContains(t, err.Error(), "private response")
			}
		})
	}
}
func TestAccountBalanceSizeAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":0,"data":{"balance":1.00,"frozen":2.00}}`))
		require.NoError(t, err)
	}))
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 8}, credentials)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountBalanceBranchInvalidResponse, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIKey)
}
