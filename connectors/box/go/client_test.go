// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

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

type recordedDownloadTransport struct {
	requests  []*http.Request
	responses []*http.Response
}

func (transport *recordedDownloadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.requests = append(transport.requests, request)
	response := transport.responses[len(transport.requests)-1]
	response.Request = request
	return response, nil
}

func downloadResponse(status int, header http.Header, body string) *http.Response {
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}
}

func recordedDownloadClient(t *testing.T, transport *recordedDownloadTransport) *Client {
	t.Helper()
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: testCredentials()}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}

func TestDownloadFileFollowsSignedBoxURLWithoutOAuthToken(t *testing.T) {
	transport := &recordedDownloadTransport{responses: []*http.Response{
		downloadResponse(http.StatusFound, http.Header{"Location": {"https://dl.boxcloud.com/d/1/signed/download"}}, ""),
		downloadResponse(http.StatusOK, nil, "Hello"),
	}}
	result, err := sdkgo.RunQuery(queryContext{}, recordedDownloadClient(t, transport).DownloadFile(), testConnection, DownloadFileInput{FileID: "12345"})
	require.NoError(t, err)
	require.Equal(t, DownloadFileBranchDownloaded, result.Branch)
	require.Equal(t, "SGVsbG8=", result.Value.ContentBase64)
	require.Len(t, transport.requests, 2)
	require.Equal(t, "Bearer test-token", transport.requests[0].Header.Get("Authorization"))
	require.Equal(t, "https://dl.boxcloud.com/d/1/signed/download", transport.requests[1].URL.String())
	require.Empty(t, transport.requests[1].Header.Get("Authorization"))
}

func TestDownloadFileRejectsUnsafeRedirects(t *testing.T) {
	for _, location := range []string{
		"https://dl.boxcloud.com.evil.example/download",
		"https://boxcloud.com/download",
		"http://dl.boxcloud.com/download",
		"https://user@dl.boxcloud.com/download",
		"https://dl.boxcloud.com:8443/download",
		"https://dl.boxcloud.com/download#fragment",
		"/relative/download",
		"",
	} {
		t.Run(location, func(t *testing.T) {
			transport := &recordedDownloadTransport{responses: []*http.Response{
				downloadResponse(http.StatusFound, http.Header{"Location": {location}}, ""),
			}}
			result, err := sdkgo.RunQuery(queryContext{}, recordedDownloadClient(t, transport).DownloadFile(), testConnection, DownloadFileInput{FileID: "12345"})
			require.NoError(t, err)
			require.Equal(t, DownloadFileBranchInvalidResponse, result.Branch)
			require.Len(t, transport.requests, 1)
		})
	}
}

func TestDownloadFileRejectsSecondRedirect(t *testing.T) {
	transport := &recordedDownloadTransport{responses: []*http.Response{
		downloadResponse(http.StatusFound, http.Header{"Location": {"https://dl.boxcloud.com/d/1/signed/download"}}, ""),
		downloadResponse(http.StatusFound, http.Header{"Location": {"https://dl.boxcloud.com/d/2/signed/download"}}, ""),
	}}
	result, err := sdkgo.RunQuery(queryContext{}, recordedDownloadClient(t, transport).DownloadFile(), testConnection, DownloadFileInput{FileID: "12345"})
	require.NoError(t, err)
	require.Equal(t, DownloadFileBranchInvalidResponse, result.Branch)
	require.Len(t, transport.requests, 2)
}

func TestDownloadFileRetriesFileNotReady(t *testing.T) {
	transport := &recordedDownloadTransport{responses: []*http.Response{
		downloadResponse(http.StatusAccepted, http.Header{"Retry-After": {"5"}}, ""),
	}}
	_, err := sdkgo.RunQuery(queryContext{}, recordedDownloadClient(t, transport).DownloadFile(), testConnection, DownloadFileInput{FileID: "12345"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}
