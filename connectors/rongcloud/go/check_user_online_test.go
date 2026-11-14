// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rongcloud

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAppKey    = "test-app-key-123"
	testAppSecret = "test-app-secret-456"
)

var testConnection = sdkgo.ConnectionRef{Provider: "rongcloud", Name: "rongcloud"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "rongcloud-provider-test" }
func (queryContext) StepExecutionID() string     { return "checkUserOnline" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: testAppKey, AppSecret: sdkgo.NewSecretString(testAppSecret)}})
	require.NoError(t, err)
	return client
}

func TestSignRequestUsesDocumentedSHA1Order(t *testing.T) {
	require.Equal(t, "12da249941f9f75c2ccb17d0c250f3101e5109df", signRequest("secret", "14314", "1408710653491"))
}
func TestCheckUserOnlineSignedRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/user/checkOnline.json", request.URL.Path)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		require.Equal(t, testAppKey, request.Header.Get("App-Key"))
		require.NoError(t, request.ParseForm())
		require.Equal(t, "user123", request.Form.Get("userId"))
		require.NotContains(t, request.Form.Encode(), testAppSecret)
		nonce := request.Header.Get("Nonce")
		require.Len(t, nonce, 18)
		_, err := hex.DecodeString(nonce)
		require.NoError(t, err)
		timestamp := request.Header.Get("Timestamp")
		milliseconds, err := strconv.ParseInt(timestamp, 10, 64)
		require.NoError(t, err)
		require.InDelta(t, time.Now().UnixMilli(), milliseconds, 10000)
		hash := sha1.Sum([]byte(testAppSecret + nonce + timestamp))
		require.Equal(t, hex.EncodeToString(hash[:]), request.Header.Get("Signature"))
		_, err = writer.Write([]byte(`{"code":200,"status":"1","userId":"private-provider-user"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).CheckUserOnline(), testConnection, CheckUserOnlineInput{UserID: "user123"})
	require.NoError(t, err)
	require.Equal(t, CheckUserOnlineBranchChecked, result.Branch)
	require.True(t, result.Value.IsOnline)
	encodedOutput, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.JSONEq(t, `{"isOnline":true}`, string(encodedOutput))
	require.NotContains(t, string(encodedOutput), "private-provider-user")
}
func TestCheckUserOnlineOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"offline", 200, `{"code":200,"status":"0"}`, CheckUserOnlineBranchChecked, false},
		{"unknown user", 200, `{"code":400,"errorMessage":"private provider data"}`, CheckUserOnlineBranchProviderRejected, false},
		{"bad signature", 200, `{"code":1004,"errorMessage":"private provider data"}`, CheckUserOnlineBranchProviderRejected, false},
		{"provider rate limit", 200, `{"code":1008}`, "", true},
		{"provider timeout", 200, `{"code":1050}`, "", true},
		{"HTTP unauthorized", 401, `private provider data`, CheckUserOnlineBranchProviderRejected, false},
		{"HTTP rate limit", 429, `private provider data`, "", true},
		{"HTTP server failure", 503, `private provider data`, "", true},
		{"malformed", 200, `{`, CheckUserOnlineBranchInvalidResponse, false},
		{"missing code", 200, `{"status":"1"}`, CheckUserOnlineBranchInvalidResponse, false},
		{"wrong status", 200, `{"code":200,"status":"2"}`, CheckUserOnlineBranchInvalidResponse, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).CheckUserOnline(), testConnection, CheckUserOnlineInput{UserID: "user123"})
			if test.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAppSecret)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.name == "offline" {
				require.False(t, result.Value.IsOnline)
			}
		})
	}
}
func TestInvalidUserIDSendsNoRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		t.Error("unexpected request")
		writer.WriteHeader(200)
	}))
	defer server.Close()
	for _, userID := range []string{"", strings.Repeat("x", 65), "a-b", "张三"} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).CheckUserOnline(), testConnection, CheckUserOnlineInput{UserID: userID})
		require.NoError(t, err)
		require.Equal(t, CheckUserOnlineBranchDefect, result.Branch)
	}
}
func TestResponseLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":200,"status":"1","extra":"` + strings.Repeat("x", 100) + `"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 30}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: testAppKey, AppSecret: sdkgo.NewSecretString(testAppSecret)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.CheckUserOnline(), testConnection, CheckUserOnlineInput{UserID: "user123"})
	require.NoError(t, err)
	require.Equal(t, CheckUserOnlineBranchInvalidResponse, result.Branch)
}
func TestTransportFailureRetriesWithoutLeakingCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, address).CheckUserOnline(), testConnection, CheckUserOnlineInput{UserID: "user123"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAppKey)
	require.NotContains(t, err.Error(), testAppSecret)
}
