// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shoplazza

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testToken = "test-shoplazza-token"

var testConnection = sdkgo.ConnectionRef{Provider: "shoplazza", Name: "shoplazza"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "shoplazza-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-shop" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func newTestClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()
	client, err := New(Config{ShopDomain: "sample.myshoplaza.com"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString(testToken)}}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func response(status int, body string, retryAfter string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Retry-After": {retryAfter}}}
}
func TestGetShopRequestAndSafeOutput(t *testing.T) {
	var calls atomic.Int32
	client := newTestClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "https", request.URL.Scheme)
		require.Equal(t, "sample.myshoplaza.com", request.URL.Host)
		require.Equal(t, "/openapi/2027-01/shop", request.URL.Path)
		require.Equal(t, []string{"id", "name", "domain", "currency"}, request.URL.Query()["fields"])
		require.Equal(t, testToken, request.Header.Get("Access-Token"))
		return response(200, `{"code":"0","message":"","data":{"id":42,"name":"Sample Shop","domain":"sample.example","currency":"USD","email":"secret@example.com","address1":"secret"}}`, ""), nil
	}))
	result, err := sdkgo.RunQuery(queryContext{}, client.GetShop(), testConnection, GetShopInput{})
	require.NoError(t, err)
	require.Equal(t, GetShopBranchFound, result.Branch)
	require.Equal(t, GetShopOutput{ID: 42, Name: "Sample Shop", Domain: "sample.example", Currency: "USD"}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestGetShopOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"unauthorized", 401, "private", GetShopBranchProviderRejected, false},
		{"missing scope", 403, "private", GetShopBranchProviderRejected, false},
		{"rate limit", 429, "private", "", true},
		{"unavailable", 503, "private", "", true},
		{"malformed", 200, "{", GetShopBranchInvalidResponse, false},
		{"missing data", 200, `{"code":"0"}`, GetShopBranchInvalidResponse, false},
		{"missing field", 200, `{"data":{"id":42,"name":"S","domain":"d"}}`, GetShopBranchInvalidResponse, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newTestClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) { return response(test.status, test.body, "2"), nil }))
			result, err := sdkgo.RunQuery(queryContext{}, client.GetShop(), testConnection, GetShopInput{})
			if test.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testToken)
				require.NotContains(t, err.Error(), "private")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}
func TestShopDomainValidation(t *testing.T) {
	for _, domain := range []string{"", "https://sample.myshoplaza.com", "sample.myshoplaza.com.evil.com", "sample.myshoplazza.com", "LOCALHOST", "sample.myshoplaza.com/path"} {
		_, err := New(Config{ShopDomain: domain}, sdkgo.StaticCredentialProvider[Credentials]{})
		require.Error(t, err, domain)
	}
}
func TestGetShopTransportFailureRetriesWithoutSecret(t *testing.T) {
	client := newTestClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, io.ErrUnexpectedEOF }))
	_, err := sdkgo.RunQuery(queryContext{}, client.GetShop(), testConnection, GetShopInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testToken)
}
