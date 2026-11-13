// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lingxing

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "lingxing", Name: "lingxing"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "lingxing-provider-test" }
func (queryContext) StepExecutionID() string     { return "store-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: sdkgo.NewSecretString("1234567890abcdef"), AppSecret: sdkgo.NewSecretString("test-secret")}})
	require.NoError(t, err)
	return client
}
func TestOfficialSignVector(t *testing.T) {
	signature, err := signStoreRequest(map[string]string{"access_token": "test-token", "app_key": "1234567890abcdef", "timestamp": "1794592800"}, "1234567890abcdef")
	require.NoError(t, err)
	require.Equal(t, "sOSgwueSNA5H+Po2ZvuJ1fx+fMiT6kAPHmT53l+aAtUC3sVlLAIV+Tr2nwCfD21Y", signature)
}
func TestListStoresTokenCacheAndSanitizedResponse(t *testing.T) {
	var tokenCalls, storeCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth-server/oauth/access-token":
			tokenCalls.Add(1)
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, r.ParseMultipartForm(4096))
			require.Equal(t, "1234567890abcdef", r.FormValue("appId"))
			require.Equal(t, "test-secret", r.FormValue("appSecret"))
			_, err := w.Write([]byte(`{"code":"200","msg":"OK","data":{"access_token":"test-token","refresh_token":"private-refresh","expires_in":7199}}`))
			require.NoError(t, err)
		case "/erp/sc/data/seller/lists":
			storeCalls.Add(1)
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
			require.Empty(t, r.URL.Query().Get("access_token"))
			require.Empty(t, r.URL.Query().Get("appSecret"))
			params := map[string]string{"access_token": "test-token", "app_key": "1234567890abcdef", "timestamp": r.URL.Query().Get("timestamp")}
			expected, err := signStoreRequest(params, "1234567890abcdef")
			require.NoError(t, err)
			require.Equal(t, expected, r.URL.Query().Get("sign"))
			_, err = w.Write([]byte(`{"code":0,"message":"success","data":[{"sid":1519,"name":"EU Store","region":"EU","country":"Spain","status":1,"account_name":"private-account","seller_id":"private-seller"}]}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	for index := 0; index < 2; index++ {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListStores(), testConnection, ListStoresInput{})
		require.NoError(t, err)
		require.Equal(t, ListStoresBranchListed, result.Branch)
		require.Equal(t, StoreList{Stores: []Store{{StoreID: 1519, Name: "EU Store", Region: "EU", Country: "Spain", Status: 1}}}, result.Value)
		encoded, err := json.Marshal(result.Value)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private-account")
		require.NotContains(t, string(encoded), "private-seller")
		require.NotContains(t, string(encoded), "test-token")
	}
	require.EqualValues(t, 1, tokenCalls.Load())
	require.EqualValues(t, 2, storeCalls.Load())
}
func TestListStoresOutcomes(t *testing.T) {
	cases := []struct {
		name        string
		tokenStatus int
		tokenBody   string
		storeStatus int
		storeBody   string
		branch      sdkgo.BranchID
		retry       bool
	}{
		{"empty", 200, `{"code":200,"data":{"access_token":"test-token","expires_in":7199}}`, 200, `{"code":0,"data":[]}`, ListStoresBranchListed, false},
		{"string expiry", 200, `{"code":"200","data":{"access_token":"test-token","expires_in":"7199"}}`, 200, `{"code":0,"data":[]}`, ListStoresBranchListed, false},
		{"invalid credentials", 200, `{"code":2001001,"data":null}`, 0, "", ListStoresBranchProviderRejected, false},
		{"token unavailable", 503, `{}`, 0, "", "", true},
		{"token malformed", 200, `{`, 0, "", ListStoresBranchInvalidResponse, false},
		{"provider rate", 200, `{"code":200,"data":{"access_token":"test-token","expires_in":7199}}`, 200, `{"code":3001008}`, "", true},
		{"store rejected", 200, `{"code":200,"data":{"access_token":"test-token","expires_in":7199}}`, 200, `{"code":3001011}`, ListStoresBranchProviderRejected, false},
		{"store unavailable", 200, `{"code":200,"data":{"access_token":"test-token","expires_in":7199}}`, 503, `{}`, "", true},
		{"store malformed", 200, `{"code":200,"data":{"access_token":"test-token","expires_in":7199}}`, 200, `{`, ListStoresBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/auth-server/oauth/access-token" {
					w.WriteHeader(tc.tokenStatus)
					_, err := w.Write([]byte(tc.tokenBody))
					require.NoError(t, err)
				} else {
					w.WriteHeader(tc.storeStatus)
					_, err := w.Write([]byte(tc.storeBody))
					require.NoError(t, err)
				}
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListStores(), testConnection, ListStoresInput{})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				if result.Failure != nil {
					require.NotContains(t, result.Failure.Message, "test-secret")
					require.NotContains(t, result.Failure.Message, "test-token")
				}
			}
		})
	}
}
func TestRedirectDoesNotForwardCredentials(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListStores(), testConnection, ListStoresInput{})
	require.NoError(t, err)
	require.Equal(t, ListStoresBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "test-secret")
	require.False(t, strings.Contains(result.Failure.Message, "1234567890abcdef"))
}

func TestTokenTransportFailureRetriesWithoutLeakingCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, endpoint).ListStores(), testConnection, ListStoresInput{})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "test-secret")
	require.NotContains(t, err.Error(), "1234567890abcdef")
}
