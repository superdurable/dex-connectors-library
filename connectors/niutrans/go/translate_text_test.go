// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package niutrans

import (
	"context"
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

const testAPIKey = "test-translation-api-key"

var testConnection = sdkgo.ConnectionRef{Provider: "niutrans", Name: "niutrans"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "niutrans-provider-test" }
func (queryContext) StepExecutionID() string     { return "translation" }
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

var testInput = TranslateTextInput{Text: "你好。", From: "zh", To: "en"}

func TestTranslationRequestAndPrivateFieldsDropped(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/NiuTransServer/translation", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		require.Empty(t, request.Header.Get("Authorization"))
		require.NoError(t, request.ParseForm())
		require.Equal(t, testAPIKey, request.PostForm.Get("apikey"))
		require.Equal(t, "你好。", request.PostForm.Get("src_text"))
		require.Equal(t, "zh", request.PostForm.Get("from"))
		require.Equal(t, "en", request.PostForm.Get("to"))
		_, err := writer.Write([]byte(`{"from":"zh","to":"en","tgt_text":"Hello.","apikey":"private","src_text":"private"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).TranslateText(), testConnection, testInput)
	require.NoError(t, err)
	require.Equal(t, TranslateTextBranchTranslated, result.Branch)
	require.Equal(t, TranslateTextOutput{TranslatedText: "Hello."}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestProviderOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty input", 200, `{"error_code":"10000","error_msg":"private"}`, TranslateTextBranchProviderRejected, false},
		{"QPS", 200, `{"error_code":"10001","error_msg":"private"}`, "", true},
		{"quota", 200, `{"error_code":"13001","error_msg":"private"}`, TranslateTextBranchProviderRejected, false},
		{"timeout", 200, `{"error_code":"13008","error_msg":"private"}`, "", true},
		{"HTTP auth", 401, `{"error":"private"}`, TranslateTextBranchProviderRejected, false},
		{"HTTP rate", 429, `{"error":"private"}`, "", true},
		{"HTTP server", 503, `{"error":"private"}`, "", true},
		{"malformed", 200, `{`, TranslateTextBranchInvalidResponse, false},
		{"missing target", 200, `{"from":"zh","to":"en"}`, TranslateTextBranchInvalidResponse, false},
		{"empty target", 200, `{"tgt_text":""}`, TranslateTextBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).TranslateText(), testConnection, testInput)
			if tc.retry {
				if tc.status == 429 || tc.status == 503 {
					var retry *dex.RetryAfterError
					require.ErrorAs(t, err, &retry)
					require.Equal(t, 2*time.Second, retry.After)
				} else {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, err, &retry)
				}
				require.NotContains(t, err.Error(), testAPIKey)
				require.NotContains(t, err.Error(), "private")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "private")
			}
		})
	}
}
func TestInvalidInputsSendNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1); writer.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for _, input := range []TranslateTextInput{{}, {Text: strings.Repeat("字", 1001), From: "zh", To: "en"}, {Text: "hello", From: "../../", To: "zh"}, {Text: "hello", From: "en", To: "en"}} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).TranslateText(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, TranslateTextBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}
func TestOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"tgt_text":"Hello."}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 4}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.TranslateText(), testConnection, testInput)
	require.NoError(t, err)
	require.Equal(t, TranslateTextBranchInvalidResponse, result.Branch)
}
