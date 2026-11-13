// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xiaoetech

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testSecretKey = "private-test-secret"

var testConnection = sdkgo.ConnectionRef{Provider: "xiaoetech", Name: "xiaoetech"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "xiaoetech-provider-test" }
func (queryContext) StepExecutionID() string     { return "root-categories" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: "app-test", ClientID: "xop-test", SecretKey: sdkgo.NewSecretString(testSecretKey)}}
	client, err := New(Config{BaseURL: baseURL, MaxResponseBytes: maxResponseBytes}, credentials)
	require.NoError(t, err)
	return client
}
func TestRootCategoriesRequestAndTokenCache(t *testing.T) {
	var tokenCalls, categoryCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "app-test", request.URL.Query().Get("app_id"))
			require.Equal(t, "xop-test", request.URL.Query().Get("client_id"))
			require.Equal(t, testSecretKey, request.URL.Query().Get("secret_key"))
			require.Equal(t, "client_credential", request.URL.Query().Get("grant_type"))
			_, err := writer.Write([]byte(`{"code":0,"msg":"success","data":{"access_token":"xe-test-token","expires_in":7200}}`))
			require.NoError(t, err)
		case "/xe.goods.category.list/2.0.0":
			categoryCalls.Add(1)
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, "application/json", request.Header.Get("Content-Type"))
			var body map[string]any
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Equal(t, "xe-test-token", body["access_token"])
			require.Equal(t, float64(0), body["parent_id"])
			_, err := writer.Write([]byte(`{"code":0,"msg":"success","data":[{"id":16510,"title":"Foods","parent_id":0,"has_children":1,"private":"omit","children":[{"id":22}]},{"id":18237,"title":"Beauty","parent_id":0,"has_children":0}]}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	for range 2 {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListRootGoodsCategories(), testConnection, ListRootGoodsCategoriesInput{})
		require.NoError(t, err)
		require.Equal(t, ListRootGoodsCategoriesBranchListed, result.Branch)
		require.Equal(t, []GoodsCategory{{ID: 16510, Title: "Foods", HasChildren: true}, {ID: 18237, Title: "Beauty", HasChildren: false}}, result.Value.Categories)
	}
	require.EqualValues(t, 1, tokenCalls.Load())
	require.EqualValues(t, 2, categoryCalls.Load())
}
func TestRootCategoriesProviderOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"permission", 200, `{"code":2017,"msg":"private-test-secret"}`, ListRootGoodsCategoriesBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"invalid token", 200, `{"code":2008}`, "", sdkgo.FailureAuthentication, true},
		{"provider busy", 200, `{"code":1001}`, "", sdkgo.FailureAvailability, true},
		{"rate code", 200, `{"code":2053}`, "", sdkgo.FailureRateLimit, true},
		{"rate HTTP", 429, `private`, "", sdkgo.FailureRateLimit, true},
		{"service HTTP", 503, `private`, "", sdkgo.FailureAvailability, true},
		{"bad JSON", 200, `{`, ListRootGoodsCategoriesBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing data", 200, `{"code":0}`, ListRootGoodsCategoriesBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"bad category", 200, `{"code":0,"data":[{"id":0,"title":"bad","parent_id":0,"has_children":1}]}`, ListRootGoodsCategoriesBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/token" {
					_, err := writer.Write([]byte(`{"code":0,"data":{"access_token":"xe-test-token","expires_in":7200}}`))
					require.NoError(t, err)
					return
				}
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListRootGoodsCategories(), testConnection, ListRootGoodsCategoriesInput{})
			if tc.retry {
				if tc.status == 200 {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, err, &retry)
					require.Equal(t, tc.kind, retry.Failure.Kind)
				} else {
					var retry *dex.RetryAfterError
					require.ErrorAs(t, err, &retry)
					require.Equal(t, 2*time.Second, retry.After)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
			}
			if err != nil {
				require.NotContains(t, err.Error(), testSecretKey)
			}
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, testSecretKey)
			}
		})
	}
}
func TestTokenFailuresAndResponseBound(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":2004,"msg":"private-test-secret"}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, tokenServer.URL, 0).ListRootGoodsCategories(), testConnection, ListRootGoodsCategoriesInput{})
	require.NoError(t, err)
	require.Equal(t, ListRootGoodsCategoriesBranchProviderRejected, result.Branch)
	require.NotContains(t, result.Failure.Message, testSecretKey)
	tokenServer.Close()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			_, err := writer.Write([]byte(`{"code":0,"data":{"access_token":"xe-test-token","expires_in":7200}}`))
			require.NoError(t, err)
			return
		}
		_, err := writer.Write([]byte(`{"code":0,"data":[{"id":1,"title":"Food","parent_id":0,"has_children":0}]}`))
		require.NoError(t, err)
	}))
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 65).ListRootGoodsCategories(), testConnection, ListRootGoodsCategoriesInput{})
	require.NoError(t, err)
	require.Equal(t, ListRootGoodsCategoriesBranchInvalidResponse, result.Branch)
	server.Close()
}

func TestExpiredTokenIsRefreshedOnNextAttempt(t *testing.T) {
	var tokenCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			number := tokenCalls.Add(1)
			_, err := writer.Write([]byte(`{"code":0,"data":{"access_token":"xe-token-` + strconv.FormatInt(int64(number), 10) + `","expires_in":7200}}`))
			require.NoError(t, err)
			return
		}
		var payload struct {
			AccessToken string `json:"access_token"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		if payload.AccessToken == "xe-token-1" {
			_, err := writer.Write([]byte(`{"code":2008}`))
			require.NoError(t, err)
			return
		}
		require.Equal(t, "xe-token-2", payload.AccessToken)
		_, err := writer.Write([]byte(`{"code":0,"data":[]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	_, err := sdkgo.RunQuery(queryContext{}, client.ListRootGoodsCategories(), testConnection, ListRootGoodsCategoriesInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListRootGoodsCategories(), testConnection, ListRootGoodsCategoriesInput{})
	require.NoError(t, err)
	require.Equal(t, ListRootGoodsCategoriesBranchListed, result.Branch)
	require.EqualValues(t, 2, tokenCalls.Load())
}
