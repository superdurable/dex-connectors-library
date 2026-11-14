// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloopen

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccountSID = "abcdefghijklmnopqrstuvwxyz012345"
const testAuthToken = "012345abcdefghijklmnopqrstuvwxyz"

var testConnection = sdkgo.ConnectionRef{Provider: "cloopen", Name: "cloopen"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "cloopen-provider-test" }
func (queryContext) StepExecutionID() string     { return "balance" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccountSID: sdkgo.NewSecretString(testAccountSID), AuthToken: sdkgo.NewSecretString(testAuthToken)}})
	require.NoError(t, err)
	return client
}
func TestSignedRequestAndPrivateFieldsDropped(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/2013-12-26/Accounts/"+testAccountSID+"/AccountInfo", request.URL.Path)
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		encodedAuthorization := request.Header.Get("Authorization")
		decodedAuthorization, err := base64.StdEncoding.DecodeString(encodedAuthorization)
		require.NoError(t, err)
		parts := strings.Split(string(decodedAuthorization), ":")
		require.Len(t, parts, 2)
		require.Equal(t, testAccountSID, parts[0])
		timestamp, err := time.Parse("20060102150405", parts[1])
		require.NoError(t, err)
		require.WithinDuration(t, time.Now().UTC(), timestamp, 5*time.Second)
		digest := md5.Sum([]byte(testAccountSID + testAuthToken + parts[1]))
		require.Equal(t, strings.ToUpper(hex.EncodeToString(digest[:])), request.URL.Query().Get("sig"))
		_, err = writer.Write([]byte(`{"statusCode":"000000","Account":{"balance":"211.00","subBalance":"112","friendlyName":"private@example.com","dateCreated":"private"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetBalance(), testConnection, GetBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetBalanceBranchFetched, result.Branch)
	require.Equal(t, GetBalanceOutput{Balance: "211.00"}, result.Value)
}
func TestProviderOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"bad signature", 200, `{"statusCode":"111109","statusMsg":"private"}`, GetBalanceBranchProviderRejected, false},
		{"concurrency", 200, `{"statusCode":"111111","statusMsg":"private"}`, "", true},
		{"internal", 200, `{"statusCode":"111001","statusMsg":"private"}`, "", true},
		{"auth HTTP", 401, `{"error":"private"}`, GetBalanceBranchProviderRejected, false},
		{"rate limit", 429, `{"error":"private"}`, "", true},
		{"server error", 503, `{"error":"private"}`, "", true},
		{"malformed", 200, `{`, GetBalanceBranchInvalidResponse, false},
		{"missing status", 200, `{"Account":{"balance":"211"}}`, GetBalanceBranchInvalidResponse, false},
		{"missing balance", 200, `{"statusCode":"000000","Account":{}}`, GetBalanceBranchInvalidResponse, false},
		{"invalid balance", 200, `{"statusCode":"000000","Account":{"balance":"-1"}}`, GetBalanceBranchInvalidResponse, false},
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
				require.NotContains(t, err.Error(), testAuthToken)
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
		_, err := writer.Write([]byte(`{"statusCode":"000000","Account":{"balance":"211"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 4}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccountSID: sdkgo.NewSecretString(testAccountSID), AuthToken: sdkgo.NewSecretString(testAuthToken)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetBalance(), testConnection, GetBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetBalanceBranchInvalidResponse, result.Branch)
}
