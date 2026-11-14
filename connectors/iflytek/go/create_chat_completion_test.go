// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package iflytek

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "iflytek", Name: "iflytek"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "iflytek-provider-test" }
func (queryContext) StepExecutionID() string     { return "create-chat-completion" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

var testInput = CreateChatCompletionInput{Prompt: "你好"}

func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{APIPassword: sdkgo.NewSecretString("spark-test-password")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}
func TestCreateChatCompletionPostsNonstreamingLiteRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v1/chat/completions", request.URL.Path)
		require.Equal(t, "Bearer spark-test-password", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.NotContains(t, string(body), "spark-test-password")
		var payload struct {
			Model     string `json:"model"`
			Stream    bool   `json:"stream"`
			MaxTokens int    `json:"max_tokens"`
			Messages  []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Equal(t, "lite", payload.Model)
		require.False(t, payload.Stream)
		require.Equal(t, 512, payload.MaxTokens)
		require.Len(t, payload.Messages, 1)
		require.Equal(t, "user", payload.Messages[0].Role)
		require.Equal(t, "你好", payload.Messages[0].Content)
		_, err = io.WriteString(writer, `{"code":0,"choices":[{"message":{"role":"assistant","content":"你好！"},"index":0}],"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).CreateChatCompletion(), testConnection, testInput)
	require.NoError(t, err)
	require.Equal(t, CreateChatCompletionBranchCompleted, result.Branch)
	require.Equal(t, ChatCompletion{Content: "你好！", Model: "lite", PromptTokens: 4, CompletionTokens: 3, TotalTokens: 7}, result.Value)
}
func TestCreateChatCompletionClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"content rejected", 200, `{"code":10013}`, CreateChatCompletionBranchProviderRejected, false},
		{"daily quota", 200, `{"code":11201}`, CreateChatCompletionBranchProviderRejected, false},
		{"concurrent limit", 200, `{"code":11202}`, "", true},
		{"user flow limit", 200, `{"code":10007}`, "", true},
		{"unauthorized", 401, `{"error":{"message":"invalid user"}}`, CreateChatCompletionBranchProviderRejected, false},
		{"rate HTTP", 429, `{}`, "", true},
		{"server HTTP", 503, `{}`, "", true},
		{"missing code", 200, `{"choices":[]}`, CreateChatCompletionBranchInvalidResponse, false},
		{"no assistant", 200, `{"code":0,"choices":[]}`, CreateChatCompletionBranchInvalidResponse, false},
		{"missing usage", 200, `{"code":0,"choices":[{"message":{"role":"assistant","content":"hello"}}]}`, CreateChatCompletionBranchInvalidResponse, false},
		{"empty text", 200, `{"code":0,"choices":[{"message":{"role":"assistant","content":""}}]}`, CreateChatCompletionBranchInvalidResponse, false},
		{"wrong role", 200, `{"code":0,"choices":[{"message":{"role":"user","content":"hello"}}]}`, CreateChatCompletionBranchInvalidResponse, false},
		{"malformed", 200, `{"code":`, CreateChatCompletionBranchInvalidResponse, false},
		{"trailing", 200, `{"code":0}{}`, CreateChatCompletionBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).CreateChatCompletion(), testConnection, testInput)
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestCreateChatCompletionBoundsAndValidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"code":0,"choices":[]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).CreateChatCompletion(), testConnection, testInput)
	require.NoError(t, err)
	require.Equal(t, CreateChatCompletionBranchInvalidResponse, result.Branch)
	for _, prompt := range []string{"", "   "} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).CreateChatCompletion(), testConnection, CreateChatCompletionInput{Prompt: prompt})
		require.NoError(t, err)
		require.Equal(t, CreateChatCompletionBranchDefect, result.Branch)
	}
	_, err = New(Config{BaseURL: "https://spark-api-open.xf-yun.com/v1"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	_, err = New(Config{Model: "generalv3.5"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	_, err = New(Config{MaxOutputTokens: 4097}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
}

func TestCreateChatCompletionRetriesTransportAndRejectsMissingCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	endpoint := server.URL
	server.Close()
	_, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, endpoint, 0).CreateChatCompletion(), testConnection, testInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.CreateChatCompletion(), testConnection, testInput)
	require.NoError(t, err)
	require.Equal(t, CreateChatCompletionBranchDefect, result.Branch)
}
