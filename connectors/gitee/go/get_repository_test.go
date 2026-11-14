// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitee

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testToken = "test-gitee-token"

var testConnection = sdkgo.ConnectionRef{Provider: "gitee", Name: "gitee"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "gitee-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-repository" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString(testToken)}})
	require.NoError(t, err)
	return client
}
func TestGetRepositoryRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/v5/repos/open-source/demo", request.URL.Path)
		require.Equal(t, testToken, request.URL.Query().Get("access_token"))
		require.Empty(t, request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"id":101,"full_name":"open-source/demo","name":"demo","description":"Example repository","html_url":"https://gitee.com/open-source/demo","private":false,"default_branch":"master","stargazers_count":5,"forks_count":2}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetRepository(), testConnection, GetRepositoryInput{Owner: "open-source", Repo: "demo"})
	require.NoError(t, err)
	require.Equal(t, GetRepositoryBranchFound, result.Branch)
	require.Equal(t, "open-source/demo", result.Value.Repository.FullName)
	require.EqualValues(t, 5, result.Value.Repository.StarsCount)
}
func TestGetRepositoryOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"not found", 404, `private provider data`, GetRepositoryBranchNotFound, false}, {"unauthorized", 401, `private provider data`, GetRepositoryBranchProviderRejected, false}, {"rate limited", 429, `private provider data`, "", true}, {"unavailable", 503, `private provider data`, "", true}, {"invalid JSON", 200, `{`, GetRepositoryBranchInvalidResponse, false}, {"invalid shape", 200, `[]`, GetRepositoryBranchInvalidResponse, false}, {"oversized", 200, strings.Repeat("x", 300000), GetRepositoryBranchInvalidResponse, false}, {"redirect", 302, `private provider data`, GetRepositoryBranchProviderRejected, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Retry-After", "2")
				if test.status == 302 {
					writer.Header().Set("Location", "https://example.com/collect?token="+testToken)
				}
				writer.WriteHeader(test.status)
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetRepository(), testConnection, GetRepositoryInput{Owner: "open-source", Repo: "demo"})
			if test.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testToken)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}
func TestInvalidInputAndCredentialsSendNoRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("invalid input or credentials must not send a request")
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetRepository(), testConnection, GetRepositoryInput{Owner: "a/b", Repo: "demo"})
	require.NoError(t, err)
	require.Equal(t, GetRepositoryBranchDefect, result.Branch)
	badClient, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString("bad token")}})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, badClient.GetRepository(), testConnection, GetRepositoryInput{Owner: "open-source", Repo: "demo"})
	require.NoError(t, err)
	require.Equal(t, GetRepositoryBranchDefect, result.Branch)
	require.NotContains(t, result.Failure.Message, "bad token")
}
