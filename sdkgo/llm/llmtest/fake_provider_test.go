// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmtest_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
)

func TestFakeProviderRecordsRequestsWithoutTheCredential(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, llm.CredentialHeader{Name: "x-api-key"}, "sk-fake-key")
	provider.EnqueueReplies(
		llmtest.FakeReply{StatusCode: http.StatusAccepted, Header: http.Header{"X-Reply": {"one"}}, Body: "a",
			StreamChunks: []llmtest.FakeStreamChunk{{Delay: 10 * time.Millisecond, Data: "b"}}},
		llmtest.FakeReply{ShouldDropConnection: true},
	)
	send := func(path string, credential string, body string) (*http.Response, error) {
		request, err := http.NewRequest(http.MethodPost, provider.BaseURL()+path, strings.NewReader(body))
		require.NoError(t, err)
		request.Header.Set("x-api-key", credential)
		return http.DefaultClient.Do(request)
	}

	response, err := send("/v1/generate?stream=true", "sk-fake-key", `{"prompt":"hi"}`)
	require.NoError(t, err)
	contents, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusAccepted, response.StatusCode)
	require.Equal(t, "one", response.Header.Get("X-Reply"))
	require.Equal(t, "ab", string(contents))

	_, err = send("/v1/generate", "wrong", `{"prompt":"sk-fake-key"}`)
	require.Error(t, err, "a dropped connection is a transport error")

	response, err = send("/v1/generate", "sk-fake-key", "{}")
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusInternalServerError, response.StatusCode, "an empty queue answers 500")

	requests := provider.Requests()
	require.Len(t, requests, 3)
	require.Equal(t, "/v1/generate?stream=true", requests[0].Path)
	require.True(t, requests[0].HasCredentialInSlot)
	require.False(t, requests[0].HasCredentialOutsideSlot)
	require.Empty(t, requests[0].Header.Values("x-api-key"), "the credential header is never recorded")
	require.False(t, requests[1].HasCredentialInSlot)
	require.True(t, requests[1].HasCredentialOutsideSlot, "the key in the body is detected")
	require.Equal(t, `{"prompt":"[REDACTED]"}`, string(requests[1].Body), "the leaked key is not recorded")
	for _, request := range requests {
		require.NotContains(t, strings.Join(request.Header.Values("x-api-key"), ""), "sk-fake-key")
	}
}
