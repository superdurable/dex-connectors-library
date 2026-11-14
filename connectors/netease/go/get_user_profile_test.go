// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package netease

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testAppKey = "test-app-key"
const testAppSecret = "test-app-secret"

var testConnection = sdkgo.ConnectionRef{Provider: "netease", Name: "netease"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "netease-provider-test" }
func (queryContext) StepExecutionID() string     { return "user-profile" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString(testAppKey), AppSecret: sdkgo.NewSecretString(testAppSecret)}})
	require.NoError(t, err)
	return client
}
func TestSignedRequestAndPrivateFieldsDropped(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/im/v2/users/sample-user", request.URL.Path)
		require.Equal(t, testAppKey, request.Header.Get("AppKey"))
		nonce := request.Header.Get("Nonce")
		require.Len(t, nonce, 32)
		currentTime := request.Header.Get("CurTime")
		unixSeconds, err := strconv.ParseInt(currentTime, 10, 64)
		require.NoError(t, err)
		require.WithinDuration(t, time.Now(), time.Unix(unixSeconds, 0), 5*time.Second)
		signature := sha1.Sum([]byte(testAppSecret + nonce + currentTime))
		require.Equal(t, hex.EncodeToString(signature[:]), request.Header.Get("CheckSum"))
		_, err = writer.Write([]byte(`{"code":200,"msg":"success","data":{"account_id":"sample-user","name":"Sample User","email":"private@example.com","mobile":"13312345678","birthday":"2000-01-01"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetUserProfile(), testConnection, GetUserProfileInput{AccountID: "sample-user"})
	require.NoError(t, err)
	require.Equal(t, GetUserProfileBranchFetched, result.Branch)
	require.Equal(t, GetUserProfileOutput{AccountID: "sample-user", Name: "Sample User"}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestProviderOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"account absent", 200, `{"code":102404,"data":{}}`, GetUserProfileBranchNotFound, false},
		{"profile absent", 200, `{"code":103404,"data":{}}`, GetUserProfileBranchNotFound, false},
		{"auth rejected", 200, `{"code":414,"msg":"secret details","data":{}}`, GetUserProfileBranchProviderRejected, false},
		{"provider failure", 200, `{"code":500,"data":{}}`, "", true},
		{"rate limited", 429, `private provider data`, "", true},
		{"unavailable", 503, `private provider data`, "", true},
		{"malformed", 200, `{`, GetUserProfileBranchInvalidResponse, false},
		{"missing code", 200, `{"data":{"account_id":"sample-user"}}`, GetUserProfileBranchInvalidResponse, false},
		{"wrong account", 200, `{"code":200,"data":{"account_id":"other"}}`, GetUserProfileBranchInvalidResponse, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetUserProfile(), testConnection, GetUserProfileInput{AccountID: "sample-user"})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAppSecret)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestInvalidInputSendsNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1); writer.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for _, accountID := range []string{"", " bad", "a/b", strings.Repeat("x", 129)} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetUserProfile(), testConnection, GetUserProfileInput{AccountID: accountID})
		require.NoError(t, err)
		require.Equal(t, GetUserProfileBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}
func TestOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":200,"data":{"account_id":"sample-user"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 4}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString(testAppKey), AppSecret: sdkgo.NewSecretString(testAppSecret)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetUserProfile(), testConnection, GetUserProfileInput{AccountID: "sample-user"})
	require.NoError(t, err)
	require.Equal(t, GetUserProfileBranchInvalidResponse, result.Branch)
}
