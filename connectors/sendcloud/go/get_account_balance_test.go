// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendcloud

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

const testAPIUser = "test-api-user"
const testAPIKey = "test-api-key"

var testConnection = sdkgo.ConnectionRef{Provider: "sendcloud", Name: "sendcloud"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "sendcloud-provider-test" }
func (queryContext) StepExecutionID() string     { return "account-balance" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIUser: sdkgo.NewSecretString(testAPIUser), APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	return client
}

func TestAccountBalanceRequestAndPIIFilter(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/apiv2/userinfo/get", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		require.NoError(t, request.ParseForm())
		require.Equal(t, testAPIUser, request.PostForm.Get("apiUser"))
		require.Equal(t, testAPIKey, request.PostForm.Get("apiKey"))
		_, err := writer.Write([]byte(`{"result":true,"statusCode":200,"message":"请求成功","info":{"avaliableBalance":40.54,"balance":44.02,"quota":2500,"todayUsedQuota":2,"email":"private@example.com","phone":"private-phone","userName":"private-name"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountBalanceBranchRetrieved, result.Branch)
	require.Equal(t, GetAccountBalanceOutput{AvailableBalance: "40.54", Balance: "44.02", DailyQuota: 2500, TodayUsedQuota: 2}, result.Value)
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
		{"authentication rejected", 200, `{"result":false,"statusCode":40005,"message":"private auth text","info":{}}`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"permission rejected", 200, `{"result":false,"statusCode":40018,"message":"private auth text","info":{}}`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"http unauthorized without envelope", 401, `private auth text`, GetAccountBalanceBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limit code", 200, `{"result":false,"statusCode":50000,"message":"private rate text","info":{}}`, "", sdkgo.FailureRateLimit, true},
		{"http rate limit", 429, `private provider text`, "", sdkgo.FailureRateLimit, true},
		{"provider unavailable", 503, `private provider text`, "", sdkgo.FailureAvailability, true},
		{"missing balance", 200, `{"result":true,"statusCode":200,"info":{"balance":44.02,"quota":2500,"todayUsedQuota":2}}`, GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"malformed", 200, `{`, GetAccountBalanceBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAPIUser)
				require.NotContains(t, err.Error(), testAPIKey)
				require.NotContains(t, err.Error(), "private rate text")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
			require.Equal(t, tc.kind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "private auth text")
		})
	}
}

func TestOversizedResponseAndTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"result":true,"statusCode":200,"info":{"avaliableBalance":40.54,"balance":44.02,"quota":2500,"todayUsedQuota":2}}`))
		require.NoError(t, err)
	}))
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIUser: sdkgo.NewSecretString(testAPIUser), APIKey: sdkgo.NewSecretString(testAPIKey)}}
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 8}, credentials)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountBalanceBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.NotContains(t, err.Error(), testAPIKey)
}
