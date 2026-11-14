// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youzan

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "youzan", Name: "youzan"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "youzan-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-shop-basic" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{AccessToken: sdkgo.NewSecretString("sample-test-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}
func TestGetShopBasicUsesOfficialRequestAndReadsShop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/youzan.shop.basic.get/3.0.0", request.URL.Path)
		require.Equal(t, "sample-test-token", request.URL.Query().Get("access_token"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.Equal(t, "{}", string(body))
		_, err = io.WriteString(writer, `{"code":200,"success":true,"message":"successful","data":{"sid":59931296,"name":"Sample Shop","role":1,"line_of_business":70000,"url":"https://h5.youzan.com/v2"}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetShopBasic(), testConnection, GetShopBasicInput{})
	require.NoError(t, err)
	require.Equal(t, GetShopBasicBranchFound, result.Branch)
	require.Equal(t, ShopBasic{ShopID: 59931296, Name: "Sample Shop", Role: 1, LineOfBusiness: 70000, URL: "https://h5.youzan.com/v2"}, result.Value)
}
func TestGetShopBasicClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"provider rejection", 200, `{"code":160100100,"success":false,"message":"rejected"}`, GetShopBasicBranchProviderRejected, false},
		{"system exception", 200, `{"code":161400100,"success":false}`, "", true},
		{"unauthorized", 401, `{}`, GetShopBasicBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing success", 200, `{"code":200,"data":{"sid":1,"name":"S"}}`, GetShopBasicBranchInvalidResponse, false},
		{"missing data", 200, `{"code":200,"success":true}`, GetShopBasicBranchInvalidResponse, false},
		{"missing shop ID", 200, `{"code":200,"success":true,"data":{"name":"S"}}`, GetShopBasicBranchInvalidResponse, false},
		{"missing shop name", 200, `{"code":200,"success":true,"data":{"sid":1}}`, GetShopBasicBranchInvalidResponse, false},
		{"malformed", 200, `{"success":`, GetShopBasicBranchInvalidResponse, false},
		{"trailing", 200, `{"code":200,"success":true,"data":{"sid":1,"name":"S"}} {}`, GetShopBasicBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetShopBasic(), testConnection, GetShopBasicInput{})
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
func TestGetShopBasicBoundsResponseAndHidesTokenFromFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"code":200,"success":true,"data":{"sid":1,"name":"S"}}`)
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).GetShopBasic(), testConnection, GetShopBasicInput{})
	require.NoError(t, err)
	require.Equal(t, GetShopBasicBranchInvalidResponse, result.Branch)
	endpoint := server.URL
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, endpoint, 0).GetShopBasic(), testConnection, GetShopBasicInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), "sample-test-token")
}
func TestGetShopBasicRejectsUnsafeConfigurationAndCredentials(t *testing.T) {
	_, err := New(Config{BaseURL: "https://open.youzanyun.com/api"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString("bad\ntoken")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetShopBasic(), testConnection, GetShopBasicInput{})
	require.NoError(t, err)
	require.Equal(t, GetShopBasicBranchDefect, result.Branch)
}
