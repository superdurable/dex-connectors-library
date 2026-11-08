// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

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

var testConnection = sdkgo.ConnectionRef{Provider: "box", Name: "box"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "box-provider-test" }
func (queryContext) StepExecutionID() string     { return "download" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func testCredentials() Credentials {
	return Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString("test-token"), RefreshToken: sdkgo.NewSecretString("refresh-token")}
}

func TestDownloadFileUsesBearerAndBoundsContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/2.0/files/12345/content", request.URL.Path)
		require.Equal(t, "Bearer test-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte("Hello"))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, MaxDownloadBytes: 5}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: testCredentials()})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.DownloadFile(), testConnection, DownloadFileInput{FileID: "12345"})
	require.NoError(t, err)
	require.Equal(t, DownloadFileBranchDownloaded, result.Branch)
	require.Equal(t, DownloadedFile{FileID: "12345", ContentBase64: "SGVsbG8=", ContentBytes: 5}, result.Value)
	require.NotContains(t, result.Value.ContentBase64, "test-token")
}

func TestDownloadFileProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		status      int
		body        string
		branch      sdkgo.BranchID
		shouldRetry bool
	}{
		{"missing", 404, `{"message":"not found"}`, DownloadFileBranchNotFound, false},
		{"forbidden", 403, `{"message":"forbidden"}`, DownloadFileBranchProviderRejected, false},
		{"rate-limited", 429, `{"message":"limit"}`, "", true},
		{"unavailable", 503, `{"message":"unavailable"}`, "", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: testCredentials()})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.DownloadFile(), testConnection, DownloadFileInput{FileID: "12345"})
			if testCase.shouldRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestDownloadFileTooLargeAndInvalidInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte("too long"))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, MaxDownloadBytes: 5}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: testCredentials()})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.DownloadFile(), testConnection, DownloadFileInput{FileID: "12345"})
	require.NoError(t, err)
	require.Equal(t, DownloadFileBranchTooLarge, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.DownloadFile(), testConnection, DownloadFileInput{FileID: "../123"})
	require.NoError(t, err)
	require.Equal(t, DownloadFileBranchDefect, result.Branch)
}
