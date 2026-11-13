// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fofa

import (
	"context"
	"encoding/base64"
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

var testConnection = sdkgo.ConnectionRef{Provider: "fofa", Name: "fofa"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "fofa-provider-test" }
func (queryContext) StepExecutionID() string     { return "search-domain-assets" }
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
	credentials := Credentials{APIKey: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func TestSearchDomainAssetsRequestAndFilteredResponse(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, assetSearchEndpoint, request.URL.Scheme+"://"+request.URL.Host+request.URL.Path)
		require.Equal(t, "test-secret", request.URL.Query().Get("key"))
		require.Equal(t, "host,ip,port", request.URL.Query().Get("fields"))
		require.Equal(t, "5", request.URL.Query().Get("size"))
		query, err := base64.StdEncoding.DecodeString(request.URL.Query().Get("qbase64"))
		require.NoError(t, err)
		require.Equal(t, `domain="example.com"`, string(query))
		require.Empty(t, request.Header.Get("Authorization"))
		return reply(200, `{"error":false,"size":9,"results":[["https://www.example.com","93.184.215.14","443"]],"query":"secret-query","errmsg":"test-secret"}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).SearchDomainAssets(), testConnection, SearchDomainAssetsInput{Domain: "example.com"})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Equal(t, SearchDomainAssetsBranchFound, result.Branch)
	require.Equal(t, SearchDomainAssetsOutput{Total: 9, Assets: []DomainAsset{{Host: "https://www.example.com", IPAddress: "93.184.215.14", Port: 443}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "test-secret")
}
func TestSearchDomainAssetsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"no hosts", 200, `{"error":false,"size":0,"results":[]}`, SearchDomainAssetsBranchFound, false},
		{"rejected", 200, `{"error":true,"errmsg":"test-secret"}`, SearchDomainAssetsBranchProviderRejected, false},
		{"bad key", 401, `{"errmsg":"test-secret"}`, SearchDomainAssetsBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true}, {"server", 503, `{}`, "", true},
		{"missing flag", 200, `{"size":1,"results":[]}`, SearchDomainAssetsBranchInvalidResponse, false},
		{"missing results", 200, `{"error":false,"size":1}`, SearchDomainAssetsBranchInvalidResponse, false},
		{"bad IP", 200, `{"error":false,"size":1,"results":[["host","bad","443"]]}`, SearchDomainAssetsBranchInvalidResponse, false},
		{"bad port", 200, `{"error":false,"size":1,"results":[["host","1.2.3.4","0"]]}`, SearchDomainAssetsBranchInvalidResponse, false},
		{"redirect", 302, `{}`, SearchDomainAssetsBranchProviderRejected, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).SearchDomainAssets(), testConnection, SearchDomainAssetsInput{Domain: "example.com"})
			require.Equal(t, 1, calls)
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, retry.Error(), "test-secret")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "test-secret")
			}
		})
	}
}
func TestSearchDomainAssetsLocalValidationAndTransport(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, `{}`), nil })
	client := newTestClient(t, transport, 0)
	for _, domain := range []string{"", "localhost", "https://example.com", "*.example.com", "EXAMPLE.COM", "a..com"} {
		result, err := sdkgo.RunQuery(queryContext{}, client.SearchDomainAssets(), testConnection, SearchDomainAssetsInput{Domain: domain})
		require.NoError(t, err)
		require.Equal(t, SearchDomainAssetsBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, strings.Repeat("x", 100)), nil })
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 16).SearchDomainAssets(), testConnection, SearchDomainAssetsInput{Domain: "example.com"})
	require.NoError(t, err)
	require.Equal(t, SearchDomainAssetsBranchInvalidResponse, result.Branch)
	transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("test-secret") })
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).SearchDomainAssets(), testConnection, SearchDomainAssetsInput{Domain: "example.com"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "test-secret")
}
