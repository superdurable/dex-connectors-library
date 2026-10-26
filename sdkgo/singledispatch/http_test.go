// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package singledispatch_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

func TestPreparedHTTPWriteDoesNotReplayAcceptedPOSTOnReusedConnection(t *testing.T) {
	var receivedWrites atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		if _, err := io.Copy(io.Discard, request.Body); err != nil {
			t.Errorf("read POST body: %v", err)
		}
		receivedWrites.Add(1)
		connection, _, err := response.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack POST connection: %v", err)
			return
		}
		if err := connection.Close(); err != nil {
			t.Errorf("close POST connection: %v", err)
		}
	}))
	defer server.Close()
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 1}}
	t.Cleanup(client.CloseIdleConnections)
	prime, err := client.Get(server.URL)
	require.NoError(t, err)
	require.NoError(t, prime.Body.Close())

	request, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("write-once"))
	require.NoError(t, err)
	request.Header.Set("Idempotency-Key", "call-1")
	require.NotNil(t, request.GetBody, "NewRequest normally enables Transport replay")
	require.NoError(t, singledispatch.PrepareHTTPWrite(request))
	require.Nil(t, request.GetBody)
	var reusedConnection atomic.Bool
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { reusedConnection.Store(info.Reused) },
	}))
	response, err := client.Do(request)
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
	require.Error(t, err, "the server accepted the POST but dropped its reply")
	require.True(t, reusedConnection.Load(), "the POST must use the primed connection")
	require.EqualValues(t, 1, receivedWrites.Load())
}

func TestPrepareBodylessHTTPWriteCannotBeReplayed(t *testing.T) {
	request, err := http.NewRequest(http.MethodPost, "https://example.test/write", nil)
	require.NoError(t, err)
	require.NoError(t, singledispatch.PrepareHTTPWrite(request))
	require.Nil(t, request.GetBody)
	require.NotNil(t, request.Body)
	require.NotEqual(t, http.NoBody, request.Body)
	require.NoError(t, request.Body.Close())
	require.Error(t, singledispatch.PrepareHTTPWrite(nil))
}

func TestPreparedBodylessHTTPWritesUseChunkedEncoding(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			transferEncoding := make(chan []string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				transferEncoding <- request.TransferEncoding
				response.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()
			request, err := http.NewRequest(method, server.URL, nil)
			require.NoError(t, err)
			require.NoError(t, singledispatch.PrepareHTTPWrite(request))
			response, err := server.Client().Do(request)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, []string{"chunked"}, <-transferEncoding)
		})
	}
}
