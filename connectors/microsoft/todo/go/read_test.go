// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todo

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "microsoft", Name: "microsoft-todo"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "todo-provider-test" }
func (queryContext) StepExecutionID() string     { return "read-todo" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}
func testClient(t *testing.T, transport roundTripFunc) *Client {
	t.Helper()
	credentials := Credentials{OAuthClientID: "sample-client-id", OAuthClientSecret: sdkgo.NewSecretString("sample-client-secret"), AccessToken: sdkgo.NewSecretString("sample-access-token"), RefreshToken: sdkgo.NewSecretString("sample-refresh-token")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func graphResponse(status int, body string, headers http.Header) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: headers}
}

func TestListTaskListsFollowsGraphPagination(t *testing.T) {
	requests := 0
	client := testClient(t, func(request *http.Request) (*http.Response, error) {
		requests++
		require.Equal(t, "graph.microsoft.com", request.URL.Host)
		require.Equal(t, "/v1.0/me/todo/lists", request.URL.Path)
		require.Equal(t, "Bearer sample-access-token", request.Header.Get("Authorization"))
		if requests == 1 {
			return graphResponse(200, `{"value":[{"id":"list-1","displayName":"Work"}],"@odata.nextLink":"https://graph.microsoft.com/v1.0/me/todo/lists?$skiptoken=next"}`, nil), nil
		}
		require.Equal(t, "next", request.URL.Query().Get("$skiptoken"))
		return graphResponse(200, `{"value":[{"id":"list-2","displayName":"Personal"}]}`, nil), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, client.ListTaskLists(), testConnection, ListTaskListsInput{})
	require.NoError(t, err)
	require.Equal(t, ListTaskListsBranchListed, result.Branch)
	require.Equal(t, []TaskList{{ID: "list-1", DisplayName: "Work"}, {ID: "list-2", DisplayName: "Personal"}}, result.Value.Lists)
	require.Equal(t, 2, requests)
}

func TestListTasksClassifiesProviderOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "listed", status: 200, body: `{"value":[{"id":"task-1","title":"Review","status":"notStarted"}]}`, branch: ListTasksBranchListed},
		{name: "missing", status: 404, branch: ListTasksBranchNotFound},
		{name: "forbidden", status: 403, branch: ListTasksBranchProviderRejected},
		{name: "throttled", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "bad JSON", status: 200, body: `{`, branch: ListTasksBranchInvalidResponse},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client := testClient(t, func(request *http.Request) (*http.Response, error) {
				require.Equal(t, "/v1.0/me/todo/lists/list-1/tasks", request.URL.Path)
				return graphResponse(testCase.status, testCase.body, nil), nil
			})
			result, err := sdkgo.RunQuery(queryContext{}, client.ListTasks(), testConnection, ListTasksInput{ListID: "list-1"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if testCase.branch == ListTasksBranchListed {
				require.Equal(t, []Task{{ID: "task-1", Title: "Review", Status: "notStarted"}}, result.Value.Tasks)
			}
		})
	}
}
func TestListTasksRejectsUnsafePaginationAndTransportFailure(t *testing.T) {
	client := testClient(t, func(*http.Request) (*http.Response, error) {
		return graphResponse(200, `{"value":[],"@odata.nextLink":"https://evil.example/steal"}`, nil), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, client.ListTasks(), testConnection, ListTasksInput{ListID: "list-1"})
	require.NoError(t, err)
	require.Equal(t, ListTasksBranchInvalidResponse, result.Branch)
	client = testClient(t, func(*http.Request) (*http.Response, error) { return nil, errors.New("network failed") })
	_, err = sdkgo.RunQuery(queryContext{}, client.ListTasks(), testConnection, ListTasksInput{ListID: "list-1"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
