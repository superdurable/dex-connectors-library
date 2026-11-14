// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kuaishou

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "kuaishou", Name: "kuaishou"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "kuaishou-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-user-videos" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AppID: "app123", AccessToken: sdkgo.NewSecretString("token456")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}
func TestListUserVideosUsesOfficialQueryAndReadsPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/openapi/photo/list", request.URL.Path)
		require.Equal(t, "app123", request.URL.Query().Get("app_id"))
		require.Equal(t, "token456", request.URL.Query().Get("access_token"))
		require.Equal(t, "20", request.URL.Query().Get("count"))
		require.Equal(t, "priorID", request.URL.Query().Get("cursor"))
		require.Empty(t, request.Header.Get("Authorization"))
		_, err := io.WriteString(writer, `{"result":1,"video_list":[{"photo_id":"3xwn3kkerxj6g9n","caption":"测试标题","cover":"https://example.test/cover","play_url":"https://example.test/video","create_time":1596629598574,"like_count":1,"comment_count":2,"view_count":3,"pending":false}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListUserVideos(), testConnection, ListUserVideosInput{Cursor: "priorID"})
	require.NoError(t, err)
	require.Equal(t, ListUserVideosBranchListed, result.Branch)
	require.Equal(t, UserVideosPage{Videos: []UserVideo{{PhotoID: "3xwn3kkerxj6g9n", Caption: "测试标题", CreatedAtMilliseconds: 1596629598574, LikeCount: 1, CommentCount: 2, ViewCount: 3}}, NextCursor: "3xwn3kkerxj6g9n"}, result.Value)
}
func TestListUserVideosClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"provider rejected", 200, `{"result":100200108,"error_msg":"token expired"}`, ListUserVideosBranchProviderRejected, false},
		{"http unauthorized", 401, `{}`, ListUserVideosBranchProviderRejected, false},
		{"rate limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"empty page", 200, `{"result":1,"video_list":[]}`, ListUserVideosBranchListed, false},
		{"missing list", 200, `{"result":1}`, ListUserVideosBranchInvalidResponse, false},
		{"missing video id", 200, `{"result":1,"video_list":[{"caption":"x"}]}`, ListUserVideosBranchInvalidResponse, false},
		{"malformed", 200, `{"result":`, ListUserVideosBranchInvalidResponse, false},
		{"trailing", 200, `{"result":1,"video_list":[]} {}`, ListUserVideosBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListUserVideos(), testConnection, ListUserVideosInput{})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, err.Error(), "token456")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestListUserVideosChecksConfigCursorAndResponseLimit(t *testing.T) {
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListUserVideos(), testConnection, ListUserVideosInput{Cursor: " bad"})
	require.NoError(t, err)
	require.Equal(t, ListUserVideosBranchDefect, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListUserVideos(), testConnection, ListUserVideosInput{})
	require.NoError(t, err)
	require.Equal(t, ListUserVideosBranchDefect, result.Branch)
	_, err = New(Config{PageSize: 201}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	_, err = New(Config{BaseURL: "https://open.kuaishou.com/private"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, strings.Repeat("x", 100))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).ListUserVideos(), testConnection, ListUserVideosInput{})
	require.NoError(t, err)
	require.Equal(t, ListUserVideosBranchInvalidResponse, result.Branch)
}
