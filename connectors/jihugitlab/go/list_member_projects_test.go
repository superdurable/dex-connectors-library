// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jihugitlab

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

const testAPIKey = "private-test-key"

var testConnection = sdkgo.ConnectionRef{Provider: "jihugitlab", Name: "jihugitlab"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "jihugitlab-provider-test" }
func (queryContext) StepExecutionID() string     { return "project-list" }
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
	client, err := New(Config{BaseURL: "https://jihulab.com", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestListMemberProjectsRequestAndPrivacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, projectsPath, request.URL.Path)
		require.Equal(t, "true", request.URL.Query().Get("membership"))
		require.Empty(t, request.URL.Query().Get("simple"))
		require.Equal(t, "2", request.URL.Query().Get("page"))
		require.Equal(t, "10", request.URL.Query().Get("per_page"))
		require.Equal(t, testAPIKey, request.Header.Get("PRIVATE-TOKEN"))
		require.Empty(t, request.Header.Get("Authorization"))
		writer.Header().Set("X-Next-Page", "3")
		_, err := writer.Write([]byte("[{\"id\":42,\"name\":\"Example Project\",\"path_with_namespace\":\"example-group/example-project\",\"owner\":{\"email\":\"private@example.com\"}}]"))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListMemberProjects(), testConnection, ListMemberProjectsInput{Page: 2, PerPage: 10})
	require.NoError(t, err)
	require.Equal(t, ListMemberProjectsBranchListed, result.Branch)
	require.Equal(t, ListMemberProjectsOutput{Projects: []MemberProject{{ID: 42, Name: "Example Project", Path: "example-group/example-project"}}, NextPage: 3}, result.Value)
}
func TestListMemberProjectsOutcomes(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		nextPage string
		branch   sdkgo.BranchID
		kind     sdkgo.FailureKind
		retry    bool
	}{
		{"unauthorized", 401, "private-test-key", "", ListMemberProjectsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"forbidden", 403, "private", "", ListMemberProjectsBranchProviderRejected, sdkgo.FailureAuthorization, false},
		{"bad request", 400, "private", "", ListMemberProjectsBranchProviderRejected, sdkgo.FailureProviderRejection, false},
		{"rate limited", 429, "private", "", "", sdkgo.FailureRateLimit, true},
		{"bad gateway", 502, "private", "", "", sdkgo.FailureAvailability, true},
		{"malformed", 200, "{", "", ListMemberProjectsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"wrong shape", 200, "{}", "", ListMemberProjectsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing ID", 200, "[{\"name\":\"A\",\"path_with_namespace\":\"a/a\"}]", "", ListMemberProjectsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"missing path", 200, "[{\"id\":1,\"name\":\"A\"}]", "", ListMemberProjectsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"bad next page", 200, "[]", "abc", ListMemberProjectsBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.Header().Set("X-Next-Page", tc.nextPage)
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListMemberProjects(), testConnection, ListMemberProjectsInput{})
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
func TestListMemberProjectsSizeValidationAndTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte("[{\"id\":1,\"name\":\"A\",\"path_with_namespace\":\"a/a\"}]"))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 8).ListMemberProjects(), testConnection, ListMemberProjectsInput{})
	require.NoError(t, err)
	require.Equal(t, ListMemberProjectsBranchInvalidResponse, result.Branch)
	for _, input := range []ListMemberProjectsInput{{Page: -1}, {PerPage: 101}, {Page: 50001}} {
		result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListMemberProjects(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListMemberProjectsBranchDefect, result.Branch)
	}
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListMemberProjects(), testConnection, ListMemberProjectsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
func TestListMemberProjectsRejectsInsecureBaseURL(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	_, err := New(Config{BaseURL: "http://jihulab.com"}, credentials)
	require.Error(t, err)
}
