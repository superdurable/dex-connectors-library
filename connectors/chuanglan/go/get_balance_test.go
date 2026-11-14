// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chuanglan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccount = "N9000123"
const testPassword = "secret-password"

var testConnection = sdkgo.ConnectionRef{Provider: "chuanglan", Name: "chuanglan"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "chuanglan-provider-test" }
func (queryContext) StepExecutionID() string     { return "balance" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {Account: sdkgo.NewSecretString(testAccount), Password: sdkgo.NewSecretString(testPassword)}})
	require.NoError(t, err)
	return client
}
func TestReadBalanceSendsCredentialsOnlyInBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/msg/balance/json", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Empty(t, request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var body map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, map[string]string{"account": testAccount, "password": testPassword}, body)
		_, err := writer.Write([]byte(`{"code":0,"balance":"953","time":"20180524170449","errorMsg":"","private":"discard"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetBalance(), testConnection, GetBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetBalanceBranchFetched, result.Branch)
	require.Equal(t, GetBalanceOutput{RemainingMessages: 953}, result.Value)
}
func TestProviderOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"string success code", 200, `{"code":"0","balance":"0"}`, GetBalanceBranchFetched, false},
		{"invalid account", 200, `{"code":"101","errorMsg":"private"}`, GetBalanceBranchProviderRejected, false},
		{"wrong password", 200, `{"code":"102","errorMsg":"private"}`, GetBalanceBranchProviderRejected, false},
		{"wrong node", 200, `{"code":"119","errorMsg":"private"}`, GetBalanceBranchProviderRejected, false},
		{"provider request error", 200, `{"code":"130","errorMsg":"private"}`, GetBalanceBranchProviderRejected, false},
		{"auth HTTP", 401, `{"error":"private"}`, GetBalanceBranchProviderRejected, false},
		{"rate limit", 429, `{"error":"private"}`, "", true},
		{"server error", 503, `{"error":"private"}`, "", true},
		{"malformed", 200, `{`, GetBalanceBranchInvalidResponse, false},
		{"missing code", 200, `{"balance":"953"}`, GetBalanceBranchInvalidResponse, false},
		{"missing balance", 200, `{"code":0}`, GetBalanceBranchInvalidResponse, false},
		{"invalid balance", 200, `{"code":0,"balance":"-1"}`, GetBalanceBranchInvalidResponse, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetBalance(), testConnection, GetBalanceInput{})
			if tc.retry {
				if tc.status == 429 || tc.status == 503 {
					var retry *dex.RetryAfterError
					require.ErrorAs(t, err, &retry)
					require.Equal(t, 2*time.Second, retry.After)
				} else {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, err, &retry)
				}
				require.NotContains(t, err.Error(), testPassword)
				require.NotContains(t, err.Error(), "private")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "private")
			}
		})
	}
}
func TestMissingCredentialsSendNoRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { t.Fatal("provider request must not be sent") }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetBalance(), testConnection, GetBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetBalanceBranchDefect, result.Branch)
}
func TestOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":0,"balance":"953"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 4}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {Account: sdkgo.NewSecretString(testAccount), Password: sdkgo.NewSecretString(testPassword)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetBalance(), testConnection, GetBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetBalanceBranchInvalidResponse, result.Branch)
}
