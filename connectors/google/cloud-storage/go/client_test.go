// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudstorage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessToken = "synthetic-storage-token"

var testConnection = sdkgo.ConnectionRef{Provider: "google-cloud-storage", Name: "google-cloud-storage"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "storage-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	credentials := Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token")}
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestUploadLimitMustFitResponseLimitAndMaximum(t *testing.T) {
	credentials := Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token")}
	provider := sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}
	for _, config := range []Config{
		{MaxUploadBytes: 65 << 20, MaxResponseBytes: 65 << 20},
		{MaxUploadBytes: 2048, MaxResponseBytes: 1024},
	} {
		_, err := New(config, provider)
		require.ErrorContains(t, err, "upload limit")
	}
}

func TestListAndDownloadObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/storage/v1/b/example-bucket/o":
			require.Equal(t, "7", r.URL.Query().Get("maxResults"))
			require.Equal(t, "reports/", r.URL.Query().Get("prefix"))
			require.Equal(t, "cursor", r.URL.Query().Get("pageToken"))
			_, err := w.Write([]byte(`{"items":[{"name":"reports/a.txt","generation":"123","metadata":{"secret":"excluded"}}],"nextPageToken":"next"}`))
			require.NoError(t, err)
		case "/storage/v1/b/example-bucket/o/reports/a.txt":
			require.Equal(t, "/storage/v1/b/example-bucket/o/reports%2Fa.txt", r.URL.EscapedPath())
			require.Equal(t, "media", r.URL.Query().Get("alt"))
			require.Equal(t, "123", r.URL.Query().Get("generation"))
			w.Header().Set("Content-Type", "text/plain")
			_, err := w.Write([]byte("hello"))
			require.NoError(t, err)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	page, err := sdkgo.RunQuery(queryContext{}, client.ListObjects(), testConnection, ListObjectsInput{Bucket: "example-bucket", Prefix: "reports/", PageSize: 7, PageToken: "cursor"})
	require.NoError(t, err)
	require.Equal(t, ListObjectsBranchListed, page.Branch)
	require.Equal(t, ObjectsPage{Objects: []Object{{Name: "reports/a.txt", Generation: "123"}}, NextPageToken: "next"}, page.Value)
	object, err := sdkgo.RunQuery(queryContext{}, client.DownloadObject(), testConnection, DownloadObjectInput{Bucket: "example-bucket", Name: "reports/a.txt", Generation: "123"})
	require.NoError(t, err)
	require.Equal(t, DownloadObjectBranchDownloaded, object.Branch)
	require.Equal(t, DownloadedObject{ContentBase64: "aGVsbG8=", ContentType: "text/plain"}, object.Value)
}

func TestInvalidInputsAndTransientResponses(t *testing.T) {
	var status atomic.Int32
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(int(status.Load())) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	page, err := sdkgo.RunQuery(queryContext{}, client.ListObjects(), testConnection, ListObjectsInput{Bucket: "bad/bucket"})
	require.NoError(t, err)
	require.Equal(t, ListObjectsBranchDefect, page.Branch)
	object, err := sdkgo.RunQuery(queryContext{}, client.DownloadObject(), testConnection, DownloadObjectInput{Bucket: "example-bucket", Name: "a", Generation: "bad"})
	require.NoError(t, err)
	require.Equal(t, DownloadObjectBranchDefect, object.Branch)
	require.Zero(t, calls.Load())
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunQuery(queryContext{}, client.ListObjects(), testConnection, ListObjectsInput{Bucket: "example-bucket"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
	status.Store(http.StatusNotFound)
	object, err = sdkgo.RunQuery(queryContext{}, client.DownloadObject(), testConnection, DownloadObjectInput{Bucket: "example-bucket", Name: "missing"})
	require.NoError(t, err)
	require.Equal(t, DownloadObjectBranchNotFound, object.Branch)
}

func TestDownloadRejectsCredentialRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://attacker.example/object")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	object, err := sdkgo.RunQuery(queryContext{}, client.DownloadObject(), testConnection, DownloadObjectInput{Bucket: "example-bucket", Name: "a"})
	require.NoError(t, err)
	require.Equal(t, DownloadObjectBranchInvalidResponse, object.Branch)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (send roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return send(request)
}

func TestDownloadFollowsGoogleMediaRedirectWithoutBearerToken(t *testing.T) {
	var calls atomic.Int32
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		if request.URL.Hostname() == "storage.googleapis.com" {
			require.Equal(t, "Bearer "+testAccessToken, request.Header.Get("Authorization"))
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://abc-apidata.googleusercontent.com/download/storage/v1/b/example-bucket/o/a"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		require.Equal(t, "abc-apidata.googleusercontent.com", request.URL.Hostname())
		require.Empty(t, request.Header.Get("Authorization"))
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/plain"}}, Body: io.NopCloser(strings.NewReader("hello"))}, nil
	})}
	credentials := Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(httpClient))
	require.NoError(t, err)
	object, err := sdkgo.RunQuery(queryContext{}, client.DownloadObject(), testConnection, DownloadObjectInput{Bucket: "example-bucket", Name: "a"})
	require.NoError(t, err)
	require.Equal(t, DownloadObjectBranchDownloaded, object.Branch)
	require.Equal(t, "aGVsbG8=", object.Value.ContentBase64)
	require.EqualValues(t, 2, calls.Load())
}

func TestReadRejectsMalformedAndOversizedProviderResponses(t *testing.T) {
	var responseBody atomic.Value
	responseBody.Store("invalid JSON")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(responseBody.Load().(string)))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	page, err := sdkgo.RunQuery(queryContext{}, client.ListObjects(), testConnection, ListObjectsInput{Bucket: "example-bucket"})
	require.NoError(t, err)
	require.Equal(t, ListObjectsBranchInvalidResponse, page.Branch)
	responseBody.Store(`{"items":[{"name":"a","generation":"invalid"}]}`)
	page, err = sdkgo.RunQuery(queryContext{}, client.ListObjects(), testConnection, ListObjectsInput{Bucket: "example-bucket"})
	require.NoError(t, err)
	require.Equal(t, ListObjectsBranchInvalidResponse, page.Branch)
	responseBody.Store("oversized")
	client.maxResponseBytes = 4
	object, err := sdkgo.RunQuery(queryContext{}, client.DownloadObject(), testConnection, DownloadObjectInput{Bucket: "example-bucket", Name: "a"})
	require.NoError(t, err)
	require.Equal(t, DownloadObjectBranchInvalidResponse, object.Branch)
}
