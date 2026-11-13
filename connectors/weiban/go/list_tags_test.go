// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package weiban

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "weiban", Name: "weiban"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "weiban-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-tags" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint, secret string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {CorpID: "corp_test", Secret: sdkgo.NewSecretString(secret)}})
	require.NoError(t, err)
	return client
}
func runTags(t *testing.T, server *httptest.Server, secret string) (sdkgo.QueryResult[TagsResult], error) {
	t.Helper()
	return sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, secret).ListTags(), testConnection, ListTagsInput{})
}
func TestListTagsProviderFlow(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/open-api/access_token/get":
			require.Equal(t, http.MethodPost, r.Method)
			require.Empty(t, r.URL.RawQuery)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"corp_id":"corp_test","secret":"private-secret"}`, string(body))
			_, err = w.Write([]byte(`{"access_token":"private-access-token","errcode":0,"errmsg":"ok","expires_in":7200}`))
			require.NoError(t, err)
		case "/open-api/tag/list":
			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "private-access-token", r.URL.Query().Get("access_token"))
			require.Equal(t, "10", r.URL.Query().Get("limit"))
			require.Equal(t, "0", r.URL.Query().Get("offset"))
			_, err := w.Write([]byte(`{"errcode":0,"errmsg":"ok","total":1,"tag_list":[{"tag_id":"tag_123","tag_name":"Returning","tag_group":"Lifecycle","department_list":["private"],"customer_phone":"private"}]}`))
			require.NoError(t, err)
		default:
			require.Fail(t, "unexpected path")
		}
	}))
	defer server.Close()
	result, err := runTags(t, server, "private-secret")
	require.NoError(t, err)
	require.Equal(t, ListTagsBranchListed, result.Branch)
	require.Equal(t, TagsResult{Total: 1, Tags: []TagMetadata{{ID: "tag_123", Name: "Returning", Group: "Lifecycle"}}}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.EqualValues(t, 2, calls.Load())
}
func TestListTagsProviderFailures(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"token rejected", "/open-api/access_token/get", 200, `{"errcode":20101,"errmsg":"bad"}`, ListTagsBranchProviderRejected, false},
		{"token busy", "/open-api/access_token/get", 200, `{"errcode":10051,"errmsg":"busy"}`, "", true},
		{"token malformed", "/open-api/access_token/get", 200, `{`, ListTagsBranchInvalidResponse, false},
		{"token limit", "/open-api/access_token/get", 429, `{}`, "", true},
		{"tag rejected", "/open-api/tag/list", 200, `{"errcode":20357,"errmsg":"limit"}`, ListTagsBranchProviderRejected, false},
		{"tag busy", "/open-api/tag/list", 200, `{"errcode":10052,"errmsg":"busy"}`, "", true},
		{"tag malformed", "/open-api/tag/list", 200, `{"errcode":0,"total":1,"tag_list":null}`, ListTagsBranchInvalidResponse, false},
		{"tag unavailable", "/open-api/tag/list", 503, `{}`, "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == tc.path {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
					return
				}
				_, _ = w.Write([]byte(`{"access_token":"private-access-token","errcode":0,"expires_in":7200}`))
			}))
			defer server.Close()
			result, err := runTags(t, server, "private-secret")
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				if result.Failure != nil {
					require.NotContains(t, result.Failure.Message, "private")
				}
			}
		})
	}
}
func TestInvalidCredentialsNeverCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	result, err := runTags(t, server, "")
	require.NoError(t, err)
	require.Equal(t, ListTagsBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardSecret(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := runTags(t, server, "private-secret")
	require.NoError(t, err)
	require.Equal(t, ListTagsBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "private-secret")
}

func TestTagRedirectDoesNotForwardAccessToken(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/open-api/access_token/get" {
			_, _ = w.Write([]byte(`{"access_token":"private-access-token","errcode":0,"expires_in":7200}`))
			return
		}
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer server.Close()
	result, err := runTags(t, server, "private-secret")
	require.NoError(t, err)
	require.Equal(t, ListTagsBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "private-access-token")
}
