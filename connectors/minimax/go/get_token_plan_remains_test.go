// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package minimax

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "sk-cp-test-minimax-key"

var testConnection = sdkgo.ConnectionRef{Provider: "minimax", Name: "minimax"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "minimax-provider-test" }
func (queryContext) StepExecutionID() string     { return "getTokenPlanRemains" }
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

func TestGetTokenPlanRemainsRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v1/token_plan/remains", request.URL.Path)
		require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		_, err := writer.Write([]byte(`{"base_resp":{"status_code":0},"model_remains":[{"model_name":"MiniMax-M*","current_interval_remaining_percent":85,"current_weekly_remaining_percent":97,"end_time":1776373200000,"weekly_end_time":1776614400000,"current_interval_status":1}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetTokenPlanRemains(), testConnection, GetTokenPlanRemainsInput{})
	require.NoError(t, err)
	require.Equal(t, GetTokenPlanRemainsBranchFetched, result.Branch)
	require.Len(t, result.Value.ModelGroups, 1)
	require.Equal(t, "MiniMax-M*", result.Value.ModelGroups[0].ModelName)
	require.Equal(t, 85.0, *result.Value.ModelGroups[0].IntervalRemainingPercent)
	require.EqualValues(t, 1776373200000, result.Value.ModelGroups[0].IntervalResetAtMillis)
}

func TestGetTokenPlanRemainsOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty plan groups", 200, `{"base_resp":{"status_code":0},"model_remains":[]}`, GetTokenPlanRemainsBranchFetched, false},
		{"invalid key HTTP", 401, `{"message":"private provider data"}`, GetTokenPlanRemainsBranchProviderRejected, false},
		{"invalid key provider status", 200, `{"base_resp":{"status_code":1004,"status_msg":"private provider data"}}`, GetTokenPlanRemainsBranchProviderRejected, false},
		{"expired or unavailable plan", 200, `{"base_resp":{"status_code":1008,"status_msg":"private provider data"}}`, GetTokenPlanRemainsBranchProviderRejected, false},
		{"provider rate limit", 200, `{"base_resp":{"status_code":1002}}`, "", true},
		{"HTTP rate limit", 429, `private provider data`, "", true},
		{"server failure", 503, `private provider data`, "", true},
		{"malformed", 200, `{`, GetTokenPlanRemainsBranchInvalidResponse, false},
		{"missing status", 200, `{"model_remains":[]}`, GetTokenPlanRemainsBranchInvalidResponse, false},
		{"missing status code", 200, `{"base_resp":{},"model_remains":[]}`, GetTokenPlanRemainsBranchInvalidResponse, false},
		{"unknown provider rejection", 200, `{"base_resp":{"status_code":2999}}`, GetTokenPlanRemainsBranchProviderRejected, false},
		{"missing groups", 200, `{"base_resp":{"status_code":0}}`, GetTokenPlanRemainsBranchInvalidResponse, false},
		{"duplicate group", 200, `{"base_resp":{"status_code":0},"model_remains":[{"model_name":"x"},{"model_name":"x"}]}`, GetTokenPlanRemainsBranchInvalidResponse, false},
		{"invalid percentage", 200, `{"base_resp":{"status_code":0},"model_remains":[{"model_name":"x","current_interval_remaining_percent":-1}]}`, GetTokenPlanRemainsBranchInvalidResponse, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(test.status)
				_, err := writer.Write([]byte(test.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetTokenPlanRemains(), testConnection, GetTokenPlanRemainsInput{})
			if test.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAPIKey)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}

func TestInvalidCredentialsSendNoRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		t.Error("unexpected request")
		writer.WriteHeader(200)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(strings.Repeat("x", 513))}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetTokenPlanRemains(), testConnection, GetTokenPlanRemainsInput{})
	require.NoError(t, err)
	require.Equal(t, GetTokenPlanRemainsBranchDefect, result.Branch)
}
func TestResponseLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"base_resp":{"status_code":0},"model_remains":[{"model_name":"` + strings.Repeat("x", 100) + `"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 30}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetTokenPlanRemains(), testConnection, GetTokenPlanRemainsInput{})
	require.NoError(t, err)
	require.Equal(t, GetTokenPlanRemainsBranchInvalidResponse, result.Branch)
}
func TestTransportFailureRetriesWithoutLeakingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, address).GetTokenPlanRemains(), testConnection, GetTokenPlanRemainsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIKey)
}
