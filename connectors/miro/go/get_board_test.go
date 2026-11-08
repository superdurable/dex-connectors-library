// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package miro

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

var testConnection = sdkgo.ConnectionRef{Provider: "miro", Name: "miro"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "miro-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-board" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("sample-miro-access-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetBoardReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/boards/uXjVOD6LSME=", request.URL.Path)
		require.Equal(t, "Bearer sample-miro-access-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"id":"uXjVOD6LSME=","name":"Example board","type":"board","description":"private details","owner":{"name":"not returned"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetBoard(), testConnection, GetBoardInput{BoardID: "uXjVOD6LSME="})
	require.NoError(t, err)
	require.Equal(t, GetBoardBranchFound, result.Branch)
	require.Equal(t, BoardMetadata{ID: "uXjVOD6LSME=", Name: "Example board"}, result.Value)
}

func TestGetBoardClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetBoardBranchNotFound},
		{name: "forbidden", status: 403, branch: GetBoardBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"id":"uXjVOD6LSME=","name":"Example board"}`, branch: GetBoardBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetBoard(), testConnection, GetBoardInput{BoardID: "uXjVOD6LSME="})
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

func TestGetBoardRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.miro.com/v2", 0)
	for _, boardID := range []string{"", "../other", "bad\nboard", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetBoard(), testConnection, GetBoardInput{BoardID: boardID})
		require.NoError(t, err)
		require.Equal(t, GetBoardBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"id":"uXjVOD6LSME=","name":"Example board","type":"board"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).GetBoard(), testConnection, GetBoardInput{BoardID: "uXjVOD6LSME="})
	require.NoError(t, err)
	require.Equal(t, GetBoardBranchInvalidResponse, result.Branch)
}
