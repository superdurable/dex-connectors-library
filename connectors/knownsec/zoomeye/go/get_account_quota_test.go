// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoomeye

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

const testAPIKey = "test-zoomeye-api-key"

var testConnection = sdkgo.ConnectionRef{Provider: "zoomeye", Name: "zoomeye"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "zoomeye-provider-test" }
func (queryContext) StepExecutionID() string     { return "account-quota" }
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
func TestAccountQuotaFiltersPrivateProfile(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v2/userinfo", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, testAPIKey, request.Header.Get("API-KEY"))
		_, err := writer.Write([]byte(`{"code":60000,"message":"success","data":{"username":"private","email":"private@example.com","phone":"+123456789","subscription":{"plan":"Premium","points":"30000","zoomeye_points":"10000000"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetAccountQuota(), testConnection, GetAccountQuotaInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountQuotaBranchFetched, result.Branch)
	require.Equal(t, GetAccountQuotaOutput{Plan: "Premium", GeneralPoints: "30000", ZoomEyePoints: "10000000"}, result.Value)
	require.NotContains(t, result.Value.Plan, "private")
	require.EqualValues(t, 1, calls.Load())
}
func TestAccountQuotaOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"bad key", 401, `{"message":"private"}`, GetAccountQuotaBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, `{"message":"private"}`, GetAccountQuotaBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"provider code", 200, `{"code":60001,"message":"private"}`, GetAccountQuotaBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limit", 429, `private response`, "", sdkgo.FailureRateLimit, true},
		{"unavailable", 503, `private response`, "", sdkgo.FailureAvailability, true},
		{"bad JSON", 200, `{`, GetAccountQuotaBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing code", 200, `{"data":{}}`, GetAccountQuotaBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing subscription", 200, `{"code":60000,"data":{}}`, GetAccountQuotaBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"invalid points", 200, `{"code":60000,"data":{"subscription":{"plan":"Premium","points":"secret","zoomeye_points":"3"}}}`, GetAccountQuotaBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetAccountQuota(), testConnection, GetAccountQuotaInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
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
func TestAccountQuotaSizeAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":60000,"data":{"subscription":{"plan":"P","points":"1","zoomeye_points":"2"}}}`))
		require.NoError(t, err)
	}))
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 8}, credentials)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetAccountQuota(), testConnection, GetAccountQuotaInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountQuotaBranchInvalidResponse, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetAccountQuota(), testConnection, GetAccountQuotaInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIKey)
}
