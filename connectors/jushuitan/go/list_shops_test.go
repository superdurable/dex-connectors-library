// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jushuitan

import (
	"context"
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

var testConnection = sdkgo.ConnectionRef{Provider: "jushuitan", Name: "jushuitan"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "jushuitan-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-shops" }
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
	credentials := Credentials{AppKey: sdkgo.NewSecretString("key-123"), AppSecret: sdkgo.NewSecretString("secret-123"), AccessToken: sdkgo.NewSecretString("token-123")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestShopSignatureFixedVector(t *testing.T) {
	fields := url.Values{"access_token": {"token-123"}, "app_key": {"key-123"}, "biz": {`{"page_index":1,"page_size":10}`}, "charset": {"utf-8"}, "timestamp": {"1700000000"}, "version": {"2"}}
	require.Equal(t, "8a10971ed20f29a0398177137d25767d", signShopRequest(fields, "secret-123"))
}
func TestListShopsRequestAndResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, shopsEndpoint, request.URL.String())
		require.Equal(t, "application/x-www-form-urlencoded;charset=UTF-8", request.Header.Get("Content-Type"))
		require.Empty(t, request.URL.RawQuery)
		require.NoError(t, request.ParseForm())
		require.Equal(t, "key-123", request.PostForm.Get("app_key"))
		require.Equal(t, "token-123", request.PostForm.Get("access_token"))
		require.Equal(t, `{"page_index":1,"page_size":10}`, request.PostForm.Get("biz"))
		require.Equal(t, "utf-8", request.PostForm.Get("charset"))
		require.Equal(t, "2", request.PostForm.Get("version"))
		require.Equal(t, signShopRequest(request.PostForm, "secret-123"), request.PostForm.Get("sign"))
		return reply(200, `{"code":0,"msg":"执行成功","data":{"page_index":1,"page_size":10,"has_next":true,"datas":[{"shop_id":10178845,"shop_name":"Example Shop","shop_site":"Own store","nick":"should not persist"}]}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListShops(), testConnection, ListShopsInput{PageIndex: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, ListShopsBranchFound, result.Branch)
	require.Equal(t, ListShopsOutput{PageIndex: 1, PageSize: 10, HasNext: true, Shops: []Shop{{ShopID: 10178845, Name: "Example Shop", Site: "Own store"}}}, result.Value)
}
func TestListShopsClassifiesProviderResponsesWithoutSecrets(t *testing.T) {
	scenarios := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"code":0,"data":{"page_index":1,"page_size":10,"has_next":false,"datas":[]}}`, ListShopsBranchFound, false},
		{"expired token", 200, `{"code":100,"msg":"token-123"}`, ListShopsBranchProviderRejected, false},
		{"IP denied", 200, `{"code":110,"msg":"secret-123"}`, ListShopsBranchProviderRejected, false},
		{"unknown code", 200, `{"code":321,"msg":"token-123"}`, ListShopsBranchProviderRejected, false},
		{"malformed", 200, `{"code":0,"data":{"datas":[]}}`, ListShopsBranchInvalidResponse, false},
		{"rate limit code", 200, `{"code":200}`, "", true},
		{"temporary code", 200, `{"code":150}`, "", true},
		{"unauthorized", 401, `{"error":"token-123"}`, ListShopsBranchProviderRejected, false},
		{"rate limit HTTP", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(scenario.status, scenario.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListShops(), testConnection, ListShopsInput{PageIndex: 1, PageSize: 10})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "token-123")
				require.NotContains(t, retry.Error(), "secret-123")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "token-123")
				require.NotContains(t, result.Failure.Message, "secret-123")
			}
		})
	}
}
func TestListShopsRejectsInvalidInputAndBoundsResponse(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	client := newTestClient(t, transport, 16)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListShops(), testConnection, ListShopsInput{PageIndex: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, ListShopsBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListShops(), testConnection, ListShopsInput{PageIndex: 0, PageSize: 101})
	require.NoError(t, err)
	require.Equal(t, ListShopsBranchDefect, result.Branch)
}
func TestListShopsTransportAndRedirectAreSecretSafe(t *testing.T) {
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("token-123 secret-123") })
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListShops(), testConnection, ListShopsInput{PageIndex: 1, PageSize: 10})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "token-123")
	require.NotContains(t, retry.Error(), "secret-123")
	calls := 0
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		response := reply(302, "")
		response.Header.Set("Location", "https://other.example/steal")
		return response, nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).ListShops(), testConnection, ListShopsInput{PageIndex: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, ListShopsBranchProviderRejected, result.Branch)
	require.Equal(t, 1, calls)
}
