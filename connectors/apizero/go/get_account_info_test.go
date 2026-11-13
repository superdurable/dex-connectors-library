// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package apizero

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "apizero", Name: "apizero"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "apizero-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-account-info" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(token any) any         { return context.Background().Value(token) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func reply(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sk_live_test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestGetAccountInfoScopeFiltering(t *testing.T) {
	cases := []struct {
		name, body string
		want       GetAccountInfoOutput
	}{
		{"account", `{"code":0,"data":{"scope":"account","balance":{"yuan":"123.4000"},"key":{"quota_remaining":99},"private":"secret"},"msg":"secret"}`, GetAccountInfoOutput{Scope: "account", BalanceYuan: stringPointer("123.4000")}},
		{"prepaid", `{"code":0,"data":{"scope":"key","balance":{"yuan":"999.0000"},"key":{"quota_remaining":42},"private":"secret"},"msg":"secret"}`, GetAccountInfoOutput{Scope: "key", KeyQuotaRemaining: int64Pointer(42)}},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, accountInfoEndpoint, request.URL.String())
				require.Empty(t, request.URL.RawQuery)
				require.Equal(t, "Bearer sk_live_test-secret", request.Header.Get("Authorization"))
				return reply(200, scenario.body), nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetAccountInfo(), testConnection, GetAccountInfoInput{})
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			require.Equal(t, GetAccountInfoBranchFound, result.Branch)
			require.Equal(t, scenario.want, result.Value)
			encoded, err := json.Marshal(result.Value)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "secret")
			if scenario.name == "prepaid" {
				require.NotContains(t, string(encoded), "999")
			}
		})
	}
}
func TestGetAccountInfoClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"invalid key", 401, `{"code":4011,"msg":"sk_live_test-secret"}`, GetAccountInfoBranchProviderRejected, false},
		{"IP whitelist", 403, `{"code":4014,"msg":"sk_live_test-secret"}`, GetAccountInfoBranchProviderRejected, false},
		{"QPS", 429, `{"code":4029,"msg":"sk_live_test-secret"}`, "", true},
		{"daily quota", 429, `{"code":4030,"msg":"sk_live_test-secret"}`, GetAccountInfoBranchProviderRejected, false},
		{"offline", 503, `{"code":4040,"msg":"sk_live_test-secret"}`, GetAccountInfoBranchProviderRejected, false},
		{"server", 500, `{"code":5000,"msg":"sk_live_test-secret"}`, "", true},
		{"gateway", 502, `{"code":5020}`, "", true},
		{"missing code", 200, `{"data":{}}`, GetAccountInfoBranchInvalidResponse, false},
		{"unknown scope", 200, `{"code":0,"data":{"scope":"other"}}`, GetAccountInfoBranchInvalidResponse, false},
		{"missing account balance", 200, `{"code":0,"data":{"scope":"account"}}`, GetAccountInfoBranchInvalidResponse, false},
		{"invalid account balance", 200, `{"code":0,"data":{"scope":"account","balance":{"yuan":"-1"}}}`, GetAccountInfoBranchInvalidResponse, false},
		{"missing prepaid quota", 200, `{"code":0,"data":{"scope":"key","key":{}}}`, GetAccountInfoBranchInvalidResponse, false},
		{"negative prepaid quota", 200, `{"code":0,"data":{"scope":"key","key":{"quota_remaining":-1}}}`, GetAccountInfoBranchInvalidResponse, false},
		{"HTTP 503 no body", 503, `{}`, "", true},
		{"HTTP 302", 302, `{}`, GetAccountInfoBranchProviderRejected, false},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			calls := 0
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				response := reply(scenario.status, scenario.body)
				if scenario.status == 302 {
					response.Header.Set("Location", "https://other.example/steal")
				}
				return response, nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetAccountInfo(), testConnection, GetAccountInfoInput{})
			require.Equal(t, 1, calls)
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "sk_live_test-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "sk_live_test-secret")
		})
	}
}
func TestGetAccountInfoResponseBoundAndTransport(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).GetAccountInfo(), testConnection, GetAccountInfoInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountInfoBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("sk_live_test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GetAccountInfo(), testConnection, GetAccountInfoInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "sk_live_test-secret")
}
func TestGetAccountInfoRejectsInvalidKeyWithoutRequest(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	credentials := Credentials{APIKey: sdkgo.NewSecretString("bad key")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetAccountInfo(), testConnection, GetAccountInfoInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccountInfoBranchDefect, result.Branch)
	require.Zero(t, calls)
}
func stringPointer(value string) *string { return &value }
func int64Pointer(value int64) *int64    { return &value }
