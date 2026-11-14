// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jingdigital

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

const testAppID = "app-test"
const testAppSecret = "secret-test"

var testConnection = sdkgo.ConnectionRef{Provider: "jingdigital", Name: "jingdigital"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "jingdigital-provider-test" }
func (queryContext) StepExecutionID() string     { return "tag-list" }
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
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: sdkgo.NewSecretString(testAppID), AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	client, err := New(Config{BaseURL: "https://jingopenapi.jingsocial.com", MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{serverURL}}))
	require.NoError(t, err)
	return client
}
func TestListTagsRequestAndPrivacy(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		switch request.URL.Path {
		case authPath:
			require.Equal(t, http.MethodPost, request.Method)
			var body map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Equal(t, map[string]string{"app_id": testAppID, "app_secret": testAppSecret}, body)
			_, err := writer.Write([]byte(`{"code":0,"data":{"access_token":"temporary-token","refresh_token":"private-refresh"}}`))
			require.NoError(t, err)
		case tagsPath:
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, testAppID, request.Header.Get("app_id"))
			require.Equal(t, "temporary-token", request.Header.Get("access_token"))
			var body struct {
				TagIDs  []int64 `json:"tag_ids"`
				Level   int     `json:"level"`
				Page    int     `json:"page"`
				PerPage int     `json:"perPage"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
			require.Empty(t, body.TagIDs)
			require.Equal(t, 1, body.Level)
			require.Equal(t, 2, body.Page)
			require.Equal(t, 10, body.PerPage)
			_, err := writer.Write([]byte(`{"code":0,"data":{"items":[{"id":166007,"name":"Example Tag","level":1,"private":"secret"}],"_meta":{"totalCount":21,"currentPage":2,"perPage":10}}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListTags(), testConnection, ListTagsInput{Page: 2, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, ListTagsBranchListed, result.Branch)
	require.Equal(t, 2, calls)
	require.Equal(t, ListTagsOutput{Tags: []TagSummary{{ID: 166007, Name: "Example Tag", Level: 1}}, Total: 21, NextPage: 3}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	for _, secret := range []string{"temporary-token", "private-refresh", testAppSecret, "secret"} {
		require.NotContains(t, string(encoded), secret)
	}
}
func TestListTagsFailures(t *testing.T) {
	cases := []struct {
		name       string
		authStatus int
		authBody   string
		dataStatus int
		dataBody   string
		branch     sdkgo.BranchID
		kind       sdkgo.FailureKind
		retry      bool
	}{
		{"bad key", 403, "private", 0, "", ListTagsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"auth rate limit", 429, "private", 0, "", "", sdkgo.FailureRateLimit, true},
		{"auth business rejection", 200, `{"code":20001,"msg":"private"}`, 0, "", ListTagsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"auth internal error", 200, `{"code":20000}`, 0, "", "", sdkgo.FailureAvailability, true},
		{"auth malformed", 200, `{"code":0,"data":{}}`, 0, "", ListTagsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"list forbidden", 200, `{"code":0,"data":{"access_token":"token"}}`, 403, "private", ListTagsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"list unavailable", 200, `{"code":0,"data":{"access_token":"token"}}`, 503, "private", "", sdkgo.FailureAvailability, true},
		{"list business rejection", 200, `{"code":0,"data":{"access_token":"token"}}`, 200, `{"code":40006,"msg":"private"}`, ListTagsBranchProviderRejected, sdkgo.FailureAuthentication, false},
		{"list rate limit", 200, `{"code":0,"data":{"access_token":"token"}}`, 200, `{"code":20002}`, "", sdkgo.FailureRateLimit, true},
		{"list malformed", 200, `{"code":0,"data":{"access_token":"token"}}`, 200, `{"code":0}`, ListTagsBranchInvalidResponse, sdkgo.FailureProtocol, false},
		{"list bad item", 200, `{"code":0,"data":{"access_token":"token"}}`, 200, `{"code":0,"data":{"items":[{"id":0,"name":"x"}],"_meta":{"totalCount":1,"currentPage":1,"perPage":20}}}`, ListTagsBranchInvalidResponse, sdkgo.FailureProtocol, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Retry-After", "2")
				if request.URL.Path == authPath {
					writer.WriteHeader(tc.authStatus)
					_, err := writer.Write([]byte(tc.authBody))
					require.NoError(t, err)
				} else {
					writer.WriteHeader(tc.dataStatus)
					_, err := writer.Write([]byte(tc.dataBody))
					require.NoError(t, err)
				}
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server, 0).ListTags(), testConnection, ListTagsInput{})
			if tc.retry {
				if tc.authStatus == 429 {
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
				require.Equal(t, tc.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, "private")
			}
		})
	}
}
func TestListTagsValidationTransportAndURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"code":0,"data":{"access_token":"token"}}`))
		require.NoError(t, err)
	}))
	client := newTestClient(t, server, 0)
	for _, input := range []ListTagsInput{{Page: -1}, {PageSize: 101}, {Page: 1000001}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListTags(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListTagsBranchDefect, result.Branch)
	}
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, client.ListTags(), testConnection, ListTagsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: sdkgo.NewSecretString(testAppID), AppSecret: sdkgo.NewSecretString(testAppSecret)}}
	_, err = New(Config{BaseURL: "http://jingopenapi.jingsocial.com"}, credentials)
	require.Error(t, err)
	_, err = New(Config{BaseURL: "https://other.example"}, credentials)
	require.Error(t, err)
}
