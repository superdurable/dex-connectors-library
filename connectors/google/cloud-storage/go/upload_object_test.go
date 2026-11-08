// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudstorage

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestUploadObjectCreatesAndConfirmsMatchingContent(t *testing.T) {
	var uploads atomic.Int32
	var objectExists atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		switch r.Method {
		case http.MethodPost:
			uploads.Add(1)
			require.Equal(t, "/upload/storage/v1/b/example-bucket/o", r.URL.Path)
			require.Equal(t, "media", r.URL.Query().Get("uploadType"))
			require.Equal(t, "reports/a.txt", r.URL.Query().Get("name"))
			require.Equal(t, "0", r.URL.Query().Get("ifGenerationMatch"))
			require.Equal(t, "text/plain", r.Header.Get("Content-Type"))
			content, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.EqualValues(t, 5, r.ContentLength)
			require.Equal(t, "hello", string(content))
			if objectExists.Load() {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			objectExists.Store(true)
			_, err = w.Write([]byte(`{"name":"reports/a.txt","generation":"123"}`))
			require.NoError(t, err)
		case http.MethodGet:
			require.Equal(t, "/storage/v1/b/example-bucket/o/reports/a.txt", r.URL.Path)
			if r.URL.Query().Get("alt") == "media" {
				require.Equal(t, "123", r.URL.Query().Get("generation"))
				_, err := w.Write([]byte("hello"))
				require.NoError(t, err)
				return
			}
			_, err := w.Write([]byte(`{"name":"reports/a.txt","generation":"123"}`))
			require.NoError(t, err)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	input := UploadObjectInput{Bucket: "example-bucket", Name: "reports/a.txt", ContentBase64: "aGVsbG8=", ContentType: "text/plain"}
	ctx := singledispatchtest.NewContext("upload-object-flow", "upload-object-step")
	created, err := sdkgo.RunMutation(ctx, client.UploadObject(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, UploadObjectBranchUploaded, created.Branch)
	require.Equal(t, Object{Name: input.Name, Generation: "123"}, created.Value)
	ctx.AdvanceAttempt()
	confirmed, err := sdkgo.RunMutation(ctx, client.UploadObject(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, UploadObjectBranchUploaded, confirmed.Branch)
	require.Equal(t, created.Value, confirmed.Value)
	require.EqualValues(t, 2, uploads.Load())
}

func TestUploadObjectConfirmsAfterLostSuccessResponse(t *testing.T) {
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if uploads.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
			} else {
				w.WriteHeader(http.StatusPreconditionFailed)
			}
			return
		}
		if r.URL.Query().Get("alt") == "media" {
			require.Equal(t, "123", r.URL.Query().Get("generation"))
			_, err := w.Write([]byte("hello"))
			require.NoError(t, err)
			return
		}
		_, err := w.Write([]byte(`{"name":"a","generation":"123"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("lost-upload-flow", "upload-step")
	input := UploadObjectInput{Bucket: "example-bucket", Name: "a", ContentBase64: "aGVsbG8=", ContentType: "text/plain"}
	_, err := sdkgo.RunMutation(ctx, client.UploadObject(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.UploadObject(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, UploadObjectBranchUploaded, result.Branch)
	require.Equal(t, Object{Name: "a", Generation: "123"}, result.Value)
	require.EqualValues(t, 2, uploads.Load())
}

func TestUploadObjectRejectsDifferentExistingContent(t *testing.T) {
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		reads.Add(1)
		if r.URL.Query().Get("alt") == "media" {
			_, err := w.Write([]byte("different"))
			require.NoError(t, err)
			return
		}
		_, err := w.Write([]byte(`{"name":"a","generation":"123"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("exists-flow", "upload-step"), client.UploadObject(), testConnection, UploadObjectInput{Bucket: "example-bucket", Name: "a", ContentBase64: "aGVsbG8=", ContentType: "text/plain"})
	require.NoError(t, err)
	require.Equal(t, UploadObjectBranchObjectExists, result.Branch)
	require.EqualValues(t, 2, reads.Load())
}

func TestUploadObjectRetriesTransientResponsesAndRejectsInvalidInput(t *testing.T) {
	var status atomic.Int32
	var uploads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		uploads.Add(1)
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("upload-retry-flow", "upload-step")
	input := UploadObjectInput{Bucket: "example-bucket", Name: "a", ContentBase64: "aGVsbG8=", ContentType: "text/plain"}
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunMutation(ctx, client.UploadObject(), testConnection, input)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		ctx.AdvanceAttempt()
	}
	bad := input
	bad.ContentBase64 = "invalid"
	result, err := sdkgo.RunMutation(ctx, client.UploadObject(), testConnection, bad)
	require.NoError(t, err)
	require.Equal(t, UploadObjectBranchDefect, result.Branch)
	require.EqualValues(t, 2, uploads.Load())
}

func TestUploadObjectRetriesTransportBeforeReceivingResponse(t *testing.T) {
	var sends atomic.Int32
	httpClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		sends.Add(1)
		return nil, errors.New("connection reset")
	})}
	credentials := Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(httpClient))
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(singledispatchtest.NewContext("upload-transport-flow", "upload-step"), client.UploadObject(), testConnection, UploadObjectInput{Bucket: "example-bucket", Name: "a", ContentBase64: "aGVsbG8=", ContentType: "text/plain"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAccessToken)
	require.EqualValues(t, 1, sends.Load())
}
