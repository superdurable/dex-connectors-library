// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package yunpian

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "yunpian", Name: "yunpian"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "yunpian-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-balance" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{APIKey: sdkgo.NewSecretString("sample-api-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}
func TestGetAccountBalanceReadsOnlyBalance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v2/user/get.json", request.URL.Path)
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.True(t, strings.HasPrefix(request.Header.Get("Content-Type"), "application/x-www-form-urlencoded"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		form, err := url.ParseQuery(string(body))
		require.NoError(t, err)
		require.Equal(t, "sample-api-key", form.Get("apikey"))
		require.Empty(t, request.Header.Get("Authorization"))
		_, err = io.WriteString(writer, `{"nick":"person","mobile":"13812341234","email":"person@example.com","balance":42.75}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountBalanceBranchRead, result.Branch)
	require.Equal(t, AccountBalance{Balance: 42.75}, result.Value)
}
func TestGetAccountBalanceClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"auth", 400, `{"code":-1,"msg":"invalid apikey"}`, GetAccountBalanceBranchProviderRejected, false},
		{"frequency", 400, `{"code":-5,"msg":"rate"}`, "", true},
		{"busy", 200, `{"code":-51,"msg":"busy"}`, "", true},
		{"unknown", 200, `{"code":-50}`, "", true},
		{"throttle", 429, `{}`, "", true},
		{"server", 503, `{}`, "", true},
		{"forbidden", 403, `{}`, GetAccountBalanceBranchProviderRejected, false},
		{"missing", 200, `{"nick":"person"}`, GetAccountBalanceBranchInvalidResponse, false},
		{"negative", 200, `{"balance":-1}`, GetAccountBalanceBranchInvalidResponse, false},
		{"wrong type", 200, `{"balance":"42"}`, GetAccountBalanceBranchInvalidResponse, false},
		{"malformed", 200, `{"balance":`, GetAccountBalanceBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestGetAccountBalanceBoundsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"balance":1000}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountBalanceBranchInvalidResponse, result.Branch)
}

func TestGetAccountBalanceRetriesTransportAndRejectsMissingCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, endpoint, 0).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountBalanceBranchDefect, result.Branch)
}
