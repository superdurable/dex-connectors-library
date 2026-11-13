// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fanyigou

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "fanyigou", Name: "fanyigou"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "fanyigou-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-account-balance" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{AppID: sdkgo.NewSecretString("test-app"), PrivateKey: sdkgo.NewSecretString("test-private-key")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}

func TestGetAccountBalanceAuthorizedRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, accountBalanceEndpoint, request.URL.String())
		require.Equal(t, "Bearer test-private-key", request.Header.Get("Authorization"))
		require.Equal(t, "application/x-www-form-urlencoded; charset=UTF-8", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		form, err := url.ParseQuery(string(body))
		require.NoError(t, err)
		require.Equal(t, "test-app", form.Get("appid"))
		require.Equal(t, "1", form.Get("type"))
		require.Len(t, form.Get("nonce_str"), 32)
		return reply(200, `{"code":100,"msg":"test-private-key","data":{"deductType":1,"balance":137684,"private":"test-private-key"}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, GetAccountBalanceBranchFound, result.Branch)
	require.Equal(t, GetAccountBalanceOutput{DeductType: 1, Balance: 137684}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "test-private-key")
}
func TestGetAccountBalanceOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"auth", 200, `{"code":101,"msg":"test-private-key"}`, GetAccountBalanceBranchProviderRejected, false},
		{"temporary", 200, `{"code":105}`, "", true},
		{"throttle code", 200, `{"code":108}`, "", true},
		{"throttle http", 429, `{}`, "", true},
		{"server", 503, `{}`, "", true},
		{"malformed", 200, `{"data":{}}`, GetAccountBalanceBranchInvalidResponse, false},
		{"missing", 200, `{"code":100,"data":{}}`, GetAccountBalanceBranchInvalidResponse, false},
		{"negative", 200, `{"code":100,"data":{"deductType":1,"balance":-1}}`, GetAccountBalanceBranchInvalidResponse, false},
		{"unknown mode", 200, `{"code":100,"data":{"deductType":2,"balance":1}}`, GetAccountBalanceBranchInvalidResponse, false},
		{"redirect", 302, `{}`, GetAccountBalanceBranchProviderRejected, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				response := reply(scenario.status, scenario.body)
				if scenario.status == 302 {
					response.Header.Set("Location", "https://other.example/steal")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-private-key")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "test-private-key")
			}
		})
	}
}
func TestGetAccountBalanceTransportAndBounds(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-private-key") })
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-private-key")
	large := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, large, 16).GetAccountBalance(), testConnection, GetAccountBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountBalanceBranchInvalidResponse, result.Branch)
}
