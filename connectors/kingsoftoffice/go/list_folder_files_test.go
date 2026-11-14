// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kingsoftoffice

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testAppID = "AK123456"
const testAppKey = "sk098765"

var testConnection = sdkgo.ConnectionRef{Provider: "kingsoftoffice", Name: "kingsoftoffice"}
var folderInput = ListFolderFilesInput{DriveID: "drive-1", ParentID: "0"}

const validFiles = `{"code":0,"msg":"success","data":{"items":[{"id":"file-1","name":"Notes.txt","type":"file","size":12}],"next_page_token":"next-page"}}`

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "wps-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-folder-files" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string, signing bool) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL, RequestSigningEnabled: signing}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppID: testAppID, AppKey: sdkgo.NewSecretString(testAppKey)}})
	require.NoError(t, err)
	return client
}
func TestKSO1PublishedVector(t *testing.T) {
	require.Equal(t, "KSO-1 AK123456:ce8df66877175e5198c8ea1362ffddf82e4941c6f25a4ca205a1ad09d0faaf03", signKSO1Request("AK123456", "sk098765", "GET", "/v7/test?key=value", "application/json", "Mon, 02 Jan 2006 15:04:05 GMT"))
}
func TestListFolderFilesTokenCacheAndSigning(t *testing.T) {
	var tokenCalls, folderCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oauth2/token":
			tokenCalls.Add(1)
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
			require.NoError(t, request.ParseForm())
			require.Equal(t, url.Values{"grant_type": {"client_credentials"}, "client_id": {testAppID}, "client_secret": {testAppKey}}, request.PostForm)
			_, err := writer.Write([]byte(`{"access_token":"token-secret","expires_in":7200,"token_type":"bearer"}`))
			require.NoError(t, err)
		case "/v7/drives/drive-1/files/0/children":
			folderCalls.Add(1)
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "Bearer token-secret", request.Header.Get("Authorization"))
			require.Equal(t, "100", request.URL.Query().Get("page_size"))
			require.Equal(t, "cursor", request.URL.Query().Get("page_token"))
			date := request.Header.Get("X-Kso-Date")
			require.NotEmpty(t, date)
			require.Equal(t, signKSO1Request(testAppID, testAppKey, "GET", request.URL.RequestURI(), "application/json", date), request.Header.Get("X-Kso-Authorization"))
			_, err := writer.Write([]byte(validFiles))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected request path %q", request.URL.Path)
			writer.WriteHeader(404)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, true)
	input := folderInput
	input.PageToken = "cursor"
	for range 2 {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListFolderFiles(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListFolderFilesBranchListed, result.Branch)
		require.Equal(t, []FileMetadata{{ID: "file-1", Name: "Notes.txt", Type: "file", SizeBytes: 12}}, result.Value.Items)
		require.Equal(t, "next-page", result.Value.NextPageToken)
	}
	require.EqualValues(t, 1, tokenCalls.Load())
	require.EqualValues(t, 2, folderCalls.Load())
}
func TestListFolderFilesWithoutSigningHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/oauth2/token" {
			_, _ = writer.Write([]byte(`{"access_token":"token-secret","expires_in":7200,"token_type":"bearer"}`))
			return
		}
		require.Empty(t, request.Header.Get("X-Kso-Date"))
		require.Empty(t, request.Header.Get("X-Kso-Authorization"))
		_, _ = writer.Write([]byte(validFiles))
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, false).ListFolderFiles(), testConnection, folderInput)
	require.NoError(t, err)
	require.Equal(t, ListFolderFilesBranchListed, result.Branch)
}
func TestListFolderFilesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"auth", 401, `{"code":40100011}`, ListFolderFilesBranchProviderRejected, false},
		{"scope", 403, `{"code":403000001}`, ListFolderFilesBranchProviderRejected, false},
		{"rate", 429, `{"code":429000001}`, "", true},
		{"server", 503, "private body", "", true},
		{"business rate", 200, `{"code":429000001}`, "", true},
		{"business denied", 200, `{"code":403000001}`, ListFolderFilesBranchProviderRejected, false},
		{"malformed", 200, "{", ListFolderFilesBranchInvalidResponse, false},
		{"missing items", 200, `{"code":0,"data":{}}`, ListFolderFilesBranchInvalidResponse, false},
		{"bad item", 200, `{"code":0,"data":{"items":[{"id":"x","name":"a","type":"file","size":-1}]}}`, ListFolderFilesBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/oauth2/token" {
					_, _ = writer.Write([]byte(`{"access_token":"token-secret","expires_in":7200,"token_type":"bearer"}`))
					return
				}
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, _ = writer.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, false).ListFolderFiles(), testConnection, folderInput)
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAppKey)
				require.NotContains(t, err.Error(), "private body")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestTokenFailureAndInvalidInputs(t *testing.T) {
	for _, status := range []int{401, 429, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(status)
				_, _ = writer.Write([]byte(`{"code":40100008}`))
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, false).ListFolderFiles(), testConnection, folderInput)
			if status == 401 {
				require.NoError(t, err)
				require.Equal(t, ListFolderFilesBranchProviderRejected, result.Branch)
			} else {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
			}
		})
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1); writer.WriteHeader(200) }))
	defer server.Close()
	for _, input := range []ListFolderFilesInput{{DriveID: "", ParentID: "0"}, {DriveID: "x/y", ParentID: "0"}, {DriveID: "x", ParentID: ""}, {DriveID: "x", ParentID: "0", PageToken: strings.Repeat("x", 2049)}} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, false).ListFolderFiles(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListFolderFilesBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}
