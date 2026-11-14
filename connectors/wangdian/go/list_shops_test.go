// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wangdian

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"
)

const testSID = "test2"
const testAppKey = "test2-xx"
const testAppSecret = "12345"

var testConnection = sdkgo.ConnectionRef{Provider: "wangdian", Name: "wangdian"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "wangdian-provider-test" }
func (queryContext) StepExecutionID() string     { return "shop-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type redirectTransport struct{ serverURL *url.URL }

func (transport redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.URL.Scheme = transport.serverURL.Scheme
	cloned.URL.Host = transport.serverURL.Host
	return http.DefaultTransport.RoundTrip(cloned)
}
func newTestClient(t *testing.T, server *httptest.Server, maxResponseBytes int64) *Client {
	t.Helper()
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {SID: sdkgo.NewSecretString(testSID), AppKey: sdkgo.NewSecretString(testAppKey), AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	client, err := New(Config{BaseURL: "https://openapi.huice.com", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestPublishedSignatureVector(t *testing.T) {
	params := map[string]string{"appkey": "test2-xx", "page_no": "0", "end_time": "2016-08-01 13:00:00", "start_time": "2016-08-01 12:00:00", "page_size": "40", "sid": "test2", "timestamp": "1470042310"}
	require.Equal(t, "ad4e6fe037ea6e3ba4768317be9d1309", signShopParams(params, "12345"))
}
func TestListShopsSignedFormAndPrivacy(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		require.Equal(t, shopsPath, request.URL.Path)
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		require.NoError(t, request.ParseForm())
		require.Equal(t, testSID, request.PostForm.Get("sid"))
		require.Equal(t, testAppKey, request.PostForm.Get("appkey"))
		require.Equal(t, "2", request.PostForm.Get("page_no"))
		require.Equal(t, "10", request.PostForm.Get("page_size"))
		require.Empty(t, request.PostForm.Get("appsecret"))
		timestamp, err := strconv.ParseInt(request.PostForm.Get("timestamp"), 10, 64)
		require.NoError(t, err)
		require.WithinDuration(t, time.Now(), time.Unix(timestamp, 0), 5*time.Minute)
		params := map[string]string{}
		for key, values := range request.PostForm {
			if key != "sign" {
				require.Len(t, values, 1)
				params[key] = values[0]
			}
		}
		require.Equal(t, signShopParams(params, testAppSecret), request.PostForm.Get("sign"))
		_, err = writer.Write([]byte(`{"code":0,"shoplist":[{"shop_id":"332","shop_no":"SHOP-332","shop_name":"Example Shop","platform_id":127,"auth_state":1,"email":"private@example.com","refresh_token":"private-token"}],"total_count":21}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListShops(), testConnection, ListShopsInput{Page: 2, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, ListShopsBranchListed, result.Branch)
	require.Equal(t, 1, calls)
	require.Equal(t, ListShopsOutput{Shops: []ShopSummary{{ID: "332", Number: "SHOP-332", Name: "Example Shop", PlatformID: 127, AuthorizationState: 1}}, Total: 21, NextPage: 0}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	for _, secret := range []string{testAppSecret, "private@example.com", "private-token"} {
		require.NotContains(t, string(encoded), secret)
	}
}
func TestListShopsFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"forbidden", 403, "private", ListShopsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"rate HTTP", 429, "private", "", sdkgo.FailureRateLimit, true},
		{"rate business", 200, `{"code":1012,"message":"private"}`, "", sdkgo.FailureRateLimit, true},
		{"daily quota", 200, `{"code":1050}`, ListShopsBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"permission", 200, `{"code":1090}`, ListShopsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"internal", 200, `{"code":1020}`, "", sdkgo.FailureAvailability, true},
		{"unavailable", 503, "private", "", sdkgo.FailureAvailability, true},
		{"malformed", 200, "{}", ListShopsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing shop id", 200, `{"code":0,"shoplist":[{"shop_no":"x","shop_name":"x","platform_id":1,"auth_state":1}],"total_count":1}`, ListShopsBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListShops(), testConnection, ListShopsInput{})
			if tc.retry {
				if tc.status == 429 {
					var retry *dex.RetryAfterError
					require.ErrorAs(t, err, &retry)
					require.Equal(t, 2*time.Second, retry.After)
				} else {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, err, &retry)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, "private")
			}
		})
	}
}
func TestListShopsValidationTransportAndURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":0,"shoplist":[],"total_count":0}`))
		require.NoError(t, err)
	}))
	client := newTestClient(t, server, 0)
	for _, input := range []ListShopsInput{{Page: -1}, {PageSize: 101}, {Page: 1000001}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListShops(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListShopsBranchDefect, result.Branch)
	}
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, client.ListShops(), testConnection, ListShopsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {SID: sdkgo.NewSecretString(testSID), AppKey: sdkgo.NewSecretString(testAppKey), AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	_, err = New(Config{BaseURL: "http://openapi.huice.com"}, credentials)
	require.Error(t, err)
	_, err = New(Config{BaseURL: "https://other.example"}, credentials)
	require.Error(t, err)
}
