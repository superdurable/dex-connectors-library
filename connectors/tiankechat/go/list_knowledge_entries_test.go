// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tiankechat

import (
	"context"
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

const testAppID = "ap_test-app"
const testAppSecret = "tks_private-test-secret"

var testConnection = sdkgo.ConnectionRef{Provider: "tiankechat", Name: "tiankechat"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "tiankechat-provider-test" }
func (queryContext) StepExecutionID() string     { return "knowledge-list" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: testAppID, AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	client, err := New(Config{BaseURL: baseURL, MaxResponseBytes: maxResponseBytes}, credentials)
	require.NoError(t, err)
	return client
}
func TestV3SignatureFixedVector(t *testing.T) {
	require.Equal(t, "40e79888e508fef3c3b220187b66a72e71c2c822716651a3f43575c0963bc547", signTiankeRequest("tks_secret123", "1789000000", "6f8a1c2e9b7d4a35", "GET", "/api/openapi/knowledge?limit=20&page=1", ""))
}
func TestKnowledgeCatalogSignedRequestAndPrivacy(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/openapi/knowledge", request.URL.Path)
		require.Equal(t, "2", request.URL.Query().Get("page"))
		require.Equal(t, "10", request.URL.Query().Get("limit"))
		require.Equal(t, testAppID, request.Header.Get("X-App-Id"))
		require.Empty(t, request.Header.Get("X-App-Secret"))
		require.Empty(t, request.Header.Get("Authorization"))
		_, err := strconv.ParseInt(request.Header.Get("X-Timestamp"), 10, 64)
		require.NoError(t, err)
		require.Len(t, request.Header.Get("X-Nonce"), 32)
		require.Equal(t, signTiankeRequest(testAppSecret, request.Header.Get("X-Timestamp"), request.Header.Get("X-Nonce"), "GET", request.URL.RequestURI(), ""), request.Header.Get("X-Signature"))
		_, err = writer.Write([]byte(`{"ok":true,"total":21,"page":2,"limit":10,"items":[{"id":128,"question":"private","answer":"private","category":"物流","active":false,"hit_count":36,"attachment":"private"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListKnowledgeEntries(), testConnection, ListKnowledgeEntriesInput{Page: 2, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, ListKnowledgeEntriesBranchListed, result.Branch)
	require.Equal(t, ListKnowledgeEntriesOutput{Entries: []KnowledgeEntry{{ID: 128, Category: "物流", Active: false, HitCount: 36}}, Total: 21, Page: 2, Limit: 10}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestKnowledgeCatalogOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"unauthorized", 401, `{"detail":"private"}`, ListKnowledgeEntriesBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, `{"detail":"private"}`, ListKnowledgeEntriesBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"provider rejection", 200, `{"ok":false}`, ListKnowledgeEntriesBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"bad request", 400, `private`, ListKnowledgeEntriesBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limited", 429, `private`, "", sdkgo.FailureRateLimit, true},
		{"server error", 503, `private`, "", sdkgo.FailureAvailability, true},
		{"malformed", 200, `{`, ListKnowledgeEntriesBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing entries", 200, `{"ok":true,"total":1,"page":1,"limit":20}`, ListKnowledgeEntriesBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing active", 200, `{"ok":true,"total":1,"page":1,"limit":20,"items":[{"id":1,"hit_count":0}]}`, ListKnowledgeEntriesBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListKnowledgeEntries(), testConnection, ListKnowledgeEntriesInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAppSecret)
			}
			if err != nil {
				require.NotContains(t, err.Error(), testAppSecret)
			}
		})
	}
}
func TestKnowledgeCatalogSizeTransportAndInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"ok":true,"total":0,"page":1,"limit":20,"items":[]}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).ListKnowledgeEntries(), testConnection, ListKnowledgeEntriesInput{})
	require.NoError(t, err)
	require.Equal(t, ListKnowledgeEntriesBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListKnowledgeEntries(), testConnection, ListKnowledgeEntriesInput{Page: -1})
	require.NoError(t, err)
	require.Equal(t, ListKnowledgeEntriesBranchDefect, result.Branch)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListKnowledgeEntries(), testConnection, ListKnowledgeEntriesInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}

func TestBaseURLRejectsPath(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{}
	for _, baseURL := range []string{"https://chat.tianke.example/api", "https://chat.tianke.example/api/", "https://chat.tianke.example/a/b"} {
		_, err := New(Config{BaseURL: baseURL}, credentials)
		require.ErrorContains(t, err, "HTTPS origin without a path", baseURL)
	}
	for _, baseURL := range []string{"https://chat.tianke.example", "https://chat.tianke.example/"} {
		client, err := New(Config{BaseURL: baseURL}, credentials)
		require.NoError(t, err, baseURL)
		require.Equal(t, "https://chat.tianke.example", client.baseURL)
	}
}
