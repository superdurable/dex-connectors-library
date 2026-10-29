// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elevenlabs

import (
	"context"
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

const testAPIKey = "test-key-123"

var testConnection = sdkgo.ConnectionRef{Provider: "elevenlabs", Name: "elevenlabs"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	return client
}

func TestListVoicesProviderRequestAndRateLimit(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/voices", r.URL.Path)
		require.Equal(t, "GET", r.Method)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_ = body
		require.Equal(t, testAPIKey, r.Header.Get("xi-api-key"))
		require.Equal(t, "2", r.URL.Query().Get("page_size"))
		if status.Load() == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, err = w.Write([]byte("private provider text"))
			require.NoError(t, err)
			return
		}
		_, err = w.Write([]byte(`{"voices":[{"voice_id":"voice1","name":"Narrator"}],"has_more":false}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListVoices(), testConnection, ListVoicesInput{PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, ListVoicesBranchListed, result.Branch)
	require.Equal(t, "voice1", result.Value.Voices[0].VoiceID)
	status.Store(1)
	_, err = sdkgo.RunQuery(queryContext{}, client.ListVoices(), testConnection, ListVoicesInput{PageSize: 2})
	var retry *dex.RetryAfterError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 2*time.Second, retry.After)
	require.NotContains(t, err.Error(), "private provider text")
	require.NotContains(t, err.Error(), testAPIKey)
}

func TestCreateSpeechProviderRequestAndRateLimit(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/text-to-speech/voice1", r.URL.Path)
		require.Equal(t, "POST", r.Method)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, testAPIKey, r.Header.Get("xi-api-key"))
		require.Contains(t, string(body), `"text":"Hello"`)
		if status.Load() == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, err = w.Write([]byte("private provider text"))
			require.NoError(t, err)
			return
		}
		_, err = w.Write([]byte("ID3audio"))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.CreateSpeech(), testConnection, CreateSpeechInput{VoiceID: "voice1", Text: "Hello"})
	require.NoError(t, err)
	require.Equal(t, CreateSpeechBranchCreated, result.Branch)
	require.Equal(t, "SUQzYXVkaW8=", result.Value.AudioBase64)
	status.Store(1)
	_, err = sdkgo.RunQuery(queryContext{}, client.CreateSpeech(), testConnection, CreateSpeechInput{VoiceID: "voice1", Text: "Hello"})
	var retry *dex.RetryAfterError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 2*time.Second, retry.After)
	require.NotContains(t, err.Error(), "private provider text")
	require.NotContains(t, err.Error(), testAPIKey)
}
