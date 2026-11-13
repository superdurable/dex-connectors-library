// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package enlvon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testKey = "enl_test-secret"

var testConnection = sdkgo.ConnectionRef{Provider: "enlvon", Name: "enlvon"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "enlvon-provider-test" }
func (queryContext) StepExecutionID() string     { return "policy-search" }
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
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testKey)}}
	client, err := New(Config{BaseURL: "https://enlvon.com", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestSearchPoliciesRequestResponseAndPrivacy(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/search", request.URL.Path)
		require.Equal(t, "分时电价", request.URL.Query().Get("q"))
		require.Equal(t, "1", request.URL.Query().Get("top_k"))
		require.Equal(t, testKey, request.Header.Get("X-API-Key"))
		require.NotContains(t, request.URL.String(), testKey)
		_, err := writer.Write([]byte(`{"query":"分时电价","count":1,"results":[{"标题":"Policy A","日期":"2026-01-01","来源":"ndrc","原文链接":"https://www.ndrc.gov.cn/example","核心要点":["omitted detailed analysis"],"电力实体":{"private":"omitted"}}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).SearchPolicies(), testConnection, SearchPoliciesInput{Query: "分时电价", TopK: 1})
	require.NoError(t, err)
	require.Equal(t, SearchPoliciesBranchFound, result.Branch)
	require.Equal(t, []PolicyCard{{Title: "Policy A", Date: "2026-01-01", Source: "ndrc", OriginalURL: "https://www.ndrc.gov.cn/example"}}, result.Value.Policies)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "omitted")
	require.NotContains(t, string(encoded), testKey)
	require.Equal(t, 1, requests)
}
func TestSearchPoliciesFailureClasses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"invalid key", 401, "private", SearchPoliciesBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"quota exhausted", 429, "private", "", sdkgo.FailureRateLimit, true},
		{"server outage", 503, "private", "", sdkgo.FailureAvailability, true},
		{"bad query", 400, "private", SearchPoliciesBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"malformed", 200, `{"count":1}`, SearchPoliciesBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"count mismatch", 200, `{"count":2,"results":[]}`, SearchPoliciesBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"empty results", 200, `{"count":0,"results":[]}`, SearchPoliciesBranchFound, "", false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).SearchPolicies(), testConnection, SearchPoliciesInput{Query: "policy"})
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
				if tc.kind != "" {
					require.Equal(t, tc.kind, result.Failure.Kind)
					require.NotContains(t, result.Failure.Message, "private")
				}
			}
		})
	}
}
func TestSearchPoliciesValidationAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"count":0,"results":[]}`))
		require.NoError(t, err)
	}))
	client := newTestClient(t, server, 0)
	for _, input := range []SearchPoliciesInput{{}, {Query: "x", TopK: 51}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.SearchPolicies(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, SearchPoliciesBranchDefect, result.Branch)
	}
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, client.SearchPolicies(), testConnection, SearchPoliciesInput{Query: "x"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testKey)}}
	_, err = New(Config{BaseURL: "http://enlvon.com"}, credentials)
	require.Error(t, err)
	_, err = New(Config{BaseURL: "https://other.example"}, credentials)
	require.Error(t, err)
}
