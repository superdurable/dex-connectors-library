// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package box

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const helloDigest = "F7FF9E8B7BB2E09B70935A5D785E0CC5D9D0ABF0"
const oldDigest = "0000000000000000000000000000000000000000"

func metadataJSON(digest string) string {
	return `{"id":"12345","type":"file","name":"example.txt","sha1":"` + digest + `","size":5}`
}

func uploadClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint, UploadBaseURL: endpoint, MaxUploadBytes: 8}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: testCredentials()})
	require.NoError(t, err)
	return client
}

func TestUploadFileReplacesExistingContentAndPreservesName(t *testing.T) {
	var getCount atomic.Int32
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer test-token", request.Header.Get("Authorization"))
		if request.Method == http.MethodGet {
			getCount.Add(1)
			require.Equal(t, "/2.0/files/12345", request.URL.Path)
			_, err := writer.Write([]byte(metadataJSON(oldDigest)))
			require.NoError(t, err)
			return
		}
		postCount.Add(1)
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/2.0/files/12345/content", request.URL.Path)
		mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		require.NoError(t, err)
		require.Equal(t, "multipart/form-data", mediaType)
		parts := multipart.NewReader(request.Body, parameters["boundary"])
		attributes, err := parts.NextPart()
		require.NoError(t, err)
		require.Equal(t, "attributes", attributes.FormName())
		var payload struct {
			Name string `json:"name"`
		}
		require.NoError(t, json.NewDecoder(attributes).Decode(&payload))
		require.Equal(t, "example.txt", payload.Name)
		file, err := parts.NextPart()
		require.NoError(t, err)
		require.Equal(t, "file", file.FormName())
		require.Equal(t, "example.txt", file.FileName())
		content, err := io.ReadAll(file)
		require.NoError(t, err)
		require.Equal(t, "Hello", string(content))
		writer.WriteHeader(http.StatusCreated)
		_, err = writer.Write([]byte(`{"entries":[` + metadataJSON(helloDigest) + `]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := uploadClient(t, server.URL)
	result, err := sdkgo.RunMutation(queryContext{}, client.UploadFile(), testConnection, UploadFileInput{FileID: "12345", ContentBase64: "SGVsbG8="})
	require.NoError(t, err)
	require.Equal(t, UploadFileBranchUploaded, result.Branch)
	require.Equal(t, UploadedFile{FileID: "12345", Name: "example.txt", SHA1: helloDigest}, result.Value)
	require.EqualValues(t, 1, getCount.Load())
	require.EqualValues(t, 1, postCount.Load())
}

func TestUploadFileSkipsAlreadyMatchingContent(t *testing.T) {
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			postCount.Add(1)
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, err := writer.Write([]byte(metadataJSON(helloDigest)))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := uploadClient(t, server.URL)
	result, err := sdkgo.RunMutation(queryContext{}, client.UploadFile(), testConnection, UploadFileInput{FileID: "12345", ContentBase64: "SGVsbG8="})
	require.NoError(t, err)
	require.Equal(t, UploadFileBranchUploaded, result.Branch)
	require.Zero(t, postCount.Load())
}

func TestUploadFileReadsBackMalformedAcceptedResponse(t *testing.T) {
	var getCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			digest := oldDigest
			if getCount.Add(1) > 1 {
				digest = helloDigest
			}
			_, err := writer.Write([]byte(metadataJSON(digest)))
			require.NoError(t, err)
			return
		}
		writer.WriteHeader(http.StatusCreated)
		_, err := writer.Write([]byte(`{"entries":[]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, uploadClient(t, server.URL).UploadFile(), testConnection, UploadFileInput{FileID: "12345", ContentBase64: "SGVsbG8="})
	require.NoError(t, err)
	require.Equal(t, UploadFileBranchUploaded, result.Branch)
	require.EqualValues(t, 2, getCount.Load())
}

func TestUploadFileConfirmsLostResponseWithoutAnotherWrite(t *testing.T) {
	var postCount atomic.Int32
	var currentDigest atomic.Value
	currentDigest.Store(oldDigest)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			_, err := writer.Write([]byte(metadataJSON(currentDigest.Load().(string))))
			require.NoError(t, err)
			return
		}
		postCount.Add(1)
		_, err := io.Copy(io.Discard, request.Body)
		require.NoError(t, err)
		currentDigest.Store(helloDigest)
		connection, _, err := writer.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	}))
	defer server.Close()
	client := uploadClient(t, server.URL)
	input := UploadFileInput{FileID: "12345", ContentBase64: "SGVsbG8="}
	result, err := sdkgo.RunMutation(queryContext{}, client.UploadFile(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, UploadFileBranchUploaded, result.Branch)
	result, err = sdkgo.RunMutation(queryContext{}, client.UploadFile(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, UploadFileBranchUploaded, result.Branch)
	require.EqualValues(t, 1, postCount.Load())
}

func TestUploadFileRetriesUnconfirmedAcceptedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			_, _ = writer.Write([]byte(metadataJSON(oldDigest)))
			return
		}
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(`{"entries":[]}`))
	}))
	defer server.Close()
	_, err := sdkgo.RunMutation(queryContext{}, uploadClient(t, server.URL).UploadFile(), testConnection, UploadFileInput{FileID: "12345", ContentBase64: "SGVsbG8="})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}

func TestUploadFileAcceptsDocumentedOKResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			_, _ = writer.Write([]byte(metadataJSON(oldDigest)))
			return
		}
		_, _ = writer.Write([]byte(`{"total_count":1,"entries":[` + metadataJSON(helloDigest) + `]}`))
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, uploadClient(t, server.URL).UploadFile(), testConnection, UploadFileInput{FileID: "12345", ContentBase64: "SGVsbG8="})
	require.NoError(t, err)
	require.Equal(t, UploadFileBranchUploaded, result.Branch)
}

func TestUploadFileRetriesServerFailureAndRejectsInvalidInput(t *testing.T) {
	var postCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			_, _ = writer.Write([]byte(metadataJSON(oldDigest)))
			return
		}
		postCount.Add(1)
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := uploadClient(t, server.URL)
	_, err := sdkgo.RunMutation(queryContext{}, client.UploadFile(), testConnection, UploadFileInput{FileID: "12345", ContentBase64: "SGVsbG8="})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.EqualValues(t, 1, postCount.Load())
	result, err := sdkgo.RunMutation(queryContext{}, client.UploadFile(), testConnection, UploadFileInput{FileID: "../12345", ContentBase64: "SGVsbG8="})
	require.NoError(t, err)
	require.Equal(t, UploadFileBranchDefect, result.Branch)
	result, err = sdkgo.RunMutation(queryContext{}, client.UploadFile(), testConnection, UploadFileInput{FileID: "12345", ContentBase64: "not base64"})
	require.NoError(t, err)
	require.Equal(t, UploadFileBranchDefect, result.Branch)
	require.EqualValues(t, 1, postCount.Load())
}

func TestUploadFileInitialMetadataBranches(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"not-found", http.StatusNotFound, `{"code":"not_found"}`, UploadFileBranchNotFound},
		{"forbidden", http.StatusForbidden, `{"code":"forbidden"}`, UploadFileBranchProviderRejected},
		{"malformed", http.StatusOK, `{"id":"12345"}`, UploadFileBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var postCount atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodPost {
					postCount.Add(1)
					return
				}
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunMutation(queryContext{}, uploadClient(t, server.URL).UploadFile(), testConnection, UploadFileInput{FileID: "12345", ContentBase64: "SGVsbG8="})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Zero(t, postCount.Load())
		})
	}
}
