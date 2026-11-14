// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package weimob

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "weimob", Name: "weimob"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "weimob-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-store-basic" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64, shopID string) *Client {
	t.Helper()
	credentials := Credentials{ClientID: "example-client-id", ClientSecret: sdkgo.NewSecretString("example-client-secret"), ShopID: shopID}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}
func TestGetStoreBasicAuthorizesCachesAndReadsOfficialFields(t *testing.T) {
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "application/json; charset=utf-8", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.Equal(t, "{}", string(body))
		switch request.URL.Path {
		case "/fuwu/b/oauth2/token":
			tokenCalls.Add(1)
			query := request.URL.Query()
			require.Equal(t, "client_credentials", query.Get("grant_type"))
			require.Equal(t, "example-client-id", query.Get("client_id"))
			require.Equal(t, "example-client-secret", query.Get("client_secret"))
			require.Equal(t, "400019", query.Get("shop_id"))
			require.Equal(t, "business_operation_system_id", query.Get("shop_type"))
			_, err = io.WriteString(writer, `{"access_token":"issued-token","expires_in":7200}`)
		case "/apigw/bos/v2.0/info/get":
			require.Equal(t, "issued-token", request.URL.Query().Get("accesstoken"))
			_, err = io.WriteString(writer, `{"code":{"errcode":"0","errmsg":"success"},"data":{"bosId":19,"bosName":"Sample Store","bosLogo":"https://example.com/store.png","desc":"Sample store"}}`)
		default:
			t.Errorf("unexpected path %s", request.URL.Path)
		}
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0, "400019")
	for range 2 {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetStoreBasic(), testConnection, GetStoreBasicInput{})
		require.NoError(t, err)
		require.Equal(t, GetStoreBasicBranchFound, result.Branch)
		require.Equal(t, StoreBasic{StoreID: 19, Name: "Sample Store", Logo: "https://example.com/store.png", Description: "Sample store"}, result.Value)
	}
	require.Equal(t, int32(1), tokenCalls.Load())
	client.tokenMu.Lock()
	client.tokenExpiresAt = time.Now()
	client.tokenMu.Unlock()
	_, err := sdkgo.RunQuery(queryContext{}, client.GetStoreBasic(), testConnection, GetStoreBasicInput{})
	require.NoError(t, err)
	require.Equal(t, int32(2), tokenCalls.Load())
}
func TestGetStoreBasicClassifiesTokenAndStoreFailures(t *testing.T) {
	for _, tc := range []struct {
		name        string
		tokenStatus int
		tokenBody   string
		storeStatus int
		storeBody   string
		branch      sdkgo.BranchID
		retry       bool
	}{
		{"token rejected", 401, `{}`, 0, "", GetStoreBasicBranchProviderRejected, false},
		{"token rate limited", 429, `{}`, 0, "", "", true},
		{"token unavailable", 503, `{}`, 0, "", "", true},
		{"token error", 200, `{"error":"invalid_client"}`, 0, "", GetStoreBasicBranchProviderRejected, false},
		{"token malformed", 200, `{"access_token":`, 0, "", GetStoreBasicBranchInvalidResponse, false},
		{"token missing expiry", 200, `{"access_token":"issued-token"}`, 0, "", GetStoreBasicBranchInvalidResponse, false},
		{"store rejected", 200, `{"access_token":"issued-token","expires_in":7200}`, 200, `{"code":{"errcode":"100001","errmsg":"rejected"}}`, GetStoreBasicBranchProviderRejected, false},
		{"store unauthorized", 200, `{"access_token":"issued-token","expires_in":7200}`, 401, `{}`, GetStoreBasicBranchProviderRejected, false},
		{"store rate limited", 200, `{"access_token":"issued-token","expires_in":7200}`, 429, `{}`, "", true},
		{"store unavailable", 200, `{"access_token":"issued-token","expires_in":7200}`, 503, `{}`, "", true},
		{"store missing status", 200, `{"access_token":"issued-token","expires_in":7200}`, 200, `{"data":{"bosId":19,"bosName":"S"}}`, GetStoreBasicBranchInvalidResponse, false},
		{"store missing ID", 200, `{"access_token":"issued-token","expires_in":7200}`, 200, `{"code":{"errcode":"0"},"data":{"bosName":"S"}}`, GetStoreBasicBranchInvalidResponse, false},
		{"store malformed", 200, `{"access_token":"issued-token","expires_in":7200}`, 200, `{"code":`, GetStoreBasicBranchInvalidResponse, false},
		{"store trailing", 200, `{"access_token":"issued-token","expires_in":7200}`, 200, `{"code":{"errcode":"0"},"data":{"bosId":19,"bosName":"S"}} {}`, GetStoreBasicBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				var status int
				var body string
				if request.URL.Path == "/fuwu/b/oauth2/token" {
					status = tc.tokenStatus
					body = tc.tokenBody
				} else {
					status = tc.storeStatus
					body = tc.storeBody
				}
				writer.WriteHeader(status)
				_, err := io.WriteString(writer, body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0, "").GetStoreBasic(), testConnection, GetStoreBasicInput{})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, err.Error(), "example-client-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestGetStoreBasicBoundsResponsesAndRejectsInvalidCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/fuwu/b/oauth2/token" {
			_, err := io.WriteString(writer, `{"access_token":"issued-token","expires_in":7200}`)
			require.NoError(t, err)
			return
		}
		_, err := io.WriteString(writer, `{"code":{"errcode":"0"},"data":{"bosId":19,"bosName":"S"}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5, "").GetStoreBasic(), testConnection, GetStoreBasicInput{})
	require.NoError(t, err)
	require.Equal(t, GetStoreBasicBranchInvalidResponse, result.Branch)
	_, err = New(Config{BaseURL: "https://dopen.weimob.com/apigw"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	client := newTestClient(t, server.URL, 0, "bad-shop")
	result, err = sdkgo.RunQuery(queryContext{}, client.GetStoreBasic(), testConnection, GetStoreBasicInput{})
	require.NoError(t, err)
	require.Equal(t, GetStoreBasicBranchDefect, result.Branch)
	require.False(t, isOpaqueCredential("bad\nsecret"))
	require.True(t, isShopID(""))
	require.False(t, isShopID(strings.Repeat("1", 21)))
}
