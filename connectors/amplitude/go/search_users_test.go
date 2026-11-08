// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package amplitude

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "amplitude", Name: "amplitude"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "amplitude-provider-test" }
func (queryContext) StepExecutionID() string     { return "search-users" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-amplitude-api-key"), SecretKey: sdkgo.NewSecretString("sample-amplitude-secret-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestSearchUsersReadsMatchedIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/2/usersearch", request.URL.Path)
		require.Equal(t, "example-user", request.URL.Query().Get("user"))
		apiKey, secretKey, ok := request.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "sample-amplitude-api-key", apiKey)
		require.Equal(t, "sample-amplitude-secret-key", secretKey)
		_, err := writer.Write([]byte(`{"matches":[{"user_id":"example-user","amplitude_id":12345}],"type":"match_user_or_device_id"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).SearchUsers(), testConnection, SearchUsersInput{User: "example-user"})
	require.NoError(t, err)
	require.Equal(t, SearchUsersBranchSearched, result.Branch)
	require.Equal(t, UserSearchPage{User: "example-user", UserIDs: []string{"example-user"}, MatchType: "match_user_or_device_id"}, result.Value)
}

func TestSearchUsersClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "forbidden", status: 403, branch: SearchUsersBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"matches":[{"user_id":""}],"type":"nomatch"}`, branch: SearchUsersBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).SearchUsers(), testConnection, SearchUsersInput{User: "example-user"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestSearchUsersRejectsInvalidInputAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://amplitude.com", 0)
	for _, user := range []string{"", " bad ", "bad\nname", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.SearchUsers(), testConnection, SearchUsersInput{User: user})
		require.NoError(t, err)
		require.Equal(t, SearchUsersBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"matches":[],"type":"nomatch"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).SearchUsers(), testConnection, SearchUsersInput{User: "example-user"})
	require.NoError(t, err)
	require.Equal(t, SearchUsersBranchInvalidResponse, result.Branch)
}
