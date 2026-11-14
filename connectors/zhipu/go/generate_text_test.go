// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zhipu

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "zhipu", Name: "zhipu"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "zhipu-test" }
func (queryContext) StepExecutionID() string     { return "generate-text" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
func newTestClient(t *testing.T, transport http.RoundTripper, limit int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("private-test-key")}
	client, err := New(Config{MaxResponseBytes: limit}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}
func TestGenerateTextRequestAndOutput(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, chatCompletionsURL, request.URL.String())
		require.Equal(t, "Bearer private-test-key", request.Header.Get("Authorization"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Equal(t, "glm-4.7-flash", payload["model"])
		require.Equal(t, false, payload["stream"])
		require.Equal(t, float64(1024), payload["max_tokens"])
		require.NotContains(t, payload, "tools")
		require.Equal(t, "hello", payload["messages"].([]any)[0].(map[string]any)["content"])
		return response(200, `{"id":"chat-1","model":"glm-4.7-flash","choices":[{"message":{"role":"assistant","content":"world"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`), nil
	})
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GenerateText(), testConnection, GenerateTextInput{Prompt: "hello"})
	require.NoError(t, err)
	require.Equal(t, GenerateTextBranchGenerated, result.Branch)
	require.Equal(t, "world", result.Value.Text)
	require.Equal(t, TokenUsage{2, 3, 5}, result.Value.Usage)
	require.Equal(t, 1, calls)
}
func TestGenerateTextClassifiesProviderOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"truncated", 200, `{"id":"chat-1","model":"glm-4.7-flash","choices":[{"message":{"role":"assistant","content":"partial"},"finish_reason":"length"}]}`, GenerateTextBranchTruncated, false},
		{"blocked", 200, `{"id":"chat-1","model":"glm-4.7-flash","choices":[{"message":{"role":"assistant","content":null},"finish_reason":"sensitive"}]}`, GenerateTextBranchBlocked, false},
		{"unauthorized", 401, `{"error":"private-test-key"}`, GenerateTextBranchProviderRejected, false},
		{"rate limited", 429, "", "", true},
		{"unavailable", 503, "", "", true},
		{"malformed", 200, `{"choices":[]}`, GenerateTextBranchInvalidResponse, false},
		{"tool call", 200, `{"id":"chat-1","model":"glm-4.7-flash","choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{}]},"finish_reason":"tool_calls"}]}`, GenerateTextBranchInvalidResponse, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return response(testCase.status, testCase.body), nil })
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, transport, 0).GenerateText(), testConnection, GenerateTextInput{Prompt: "hello"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if testCase.name == "unauthorized" {
				require.NotContains(t, result.Failure.Message, "private-test-key")
			}
		})
	}
}
func TestGenerateTextInputBoundsAndTransportFailure(t *testing.T) {
	calls := 0
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(200, strings.Repeat("x", 200)), nil
	})
	client := newTestClient(t, transport, 64)
	for _, input := range []GenerateTextInput{{}, {Prompt: "hello", MaxOutputTokens: 4097}, {Prompt: strings.Repeat("x", 65537)}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GenerateText(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, GenerateTextBranchDefect, result.Branch)
	}
	require.Zero(t, calls)
	result, err := sdkgo.RunQuery(queryContext{}, client.GenerateText(), testConnection, GenerateTextInput{Prompt: "hello"})
	require.NoError(t, err)
	require.Equal(t, GenerateTextBranchInvalidResponse, result.Branch)
	failing := newTestClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private-test-key") }), 0)
	_, err = sdkgo.RunQuery(queryContext{}, failing.GenerateText(), testConnection, GenerateTextInput{Prompt: "hello"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, retry.Error(), "private-test-key")
}
