// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fit2cloud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "pat_private-test-key"

var testConnection = sdkgo.ConnectionRef{Provider: "fit2cloud", Name: "fit2cloud"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "fit2cloud-provider-test" }
func (queryContext) StepExecutionID() string     { return "post-list" }
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
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	client, err := New(Config{BaseURL: "https://halo.example.com", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestListPostsRequestAndPrivacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, postsPath, request.URL.Path)
		require.Empty(t, request.URL.Query().Get("membership"))

		require.Equal(t, "2", request.URL.Query().Get("page"))
		require.Equal(t, "10", request.URL.Query().Get("size"))
		require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
		require.Empty(t, request.Header.Get("PRIVATE-TOKEN"))

		_, err := writer.Write([]byte(`{"items":[{"metadata":{"name":"example-post"},"spec":{"title":"Example Post","slug":"example-post","publish":true,"visible":"PUBLIC","owner":"private-person"}}],"hasNext":true,"page":2,"size":10,"total":11}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListPosts(), testConnection, ListPostsInput{Page: 2, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, ListPostsBranchListed, result.Branch)
	require.Equal(t, ListPostsOutput{Posts: []PostSummary{{Name: "example-post", Title: "Example Post", Slug: "example-post", Published: true, Visibility: "PUBLIC"}}, NextPage: 3}, result.Value)
}
func TestListPostsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{"unauthorized", 401, "pat_private-test-key", ListPostsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, "private", ListPostsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"bad request", 400, "private", ListPostsBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limited", 429, "private", "", sdkgo.FailureRateLimit, true},
		{"bad gateway", 502, "private", "", sdkgo.FailureAvailability, true},
		{"malformed", 200, "{", ListPostsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"wrong shape", 200, "{}", ListPostsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing title", 200, `{"items":[{"metadata":{"name":"one"},"spec":{"slug":"one","visible":"PUBLIC"}}],"hasNext":false,"page":1,"size":20,"total":1}`, ListPostsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing slug", 200, `{"items":[{"metadata":{"name":"one"},"spec":{"title":"One","visible":"PUBLIC"}}],"hasNext":false,"page":1,"size":20,"total":1}`, ListPostsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"wrong page", 200, `{"items":[],"hasNext":false,"page":2,"size":20,"total":0}`, ListPostsBranchInvalidResponse, sdkgo.FailureProtocol, false},
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
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListPosts(), testConnection, ListPostsInput{})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, testAPIKey)
			}
			if err != nil {
				require.NotContains(t, err.Error(), testAPIKey)
			}
		})
	}
}
func TestListPostsSizeValidationAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"items":[],"hasNext":false,"page":1,"size":20,"total":0}`))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 8).ListPosts(), testConnection, ListPostsInput{})
	require.NoError(t, err)
	require.Equal(t, ListPostsBranchInvalidResponse, result.Branch)
	for _, input := range []ListPostsInput{{Page: -1}, {PageSize: 101}, {Page: 50001}} {
		result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListPosts(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListPostsBranchDefect, result.Branch)
	}
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListPosts(), testConnection, ListPostsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
func TestListPostsRejectsInsecureBaseURL(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	_, err := New(Config{BaseURL: "http://halo.example.com"}, credentials)
	require.Error(t, err)
}
