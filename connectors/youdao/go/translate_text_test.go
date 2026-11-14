// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youdao

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "youdao", Name: "youdao"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "youdao-provider-test" }
func (queryContext) StepExecutionID() string     { return "translate-text" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

var testInput = TranslateTextInput{Text: "你好，世界", SourceLanguage: "zh-CHS", TargetLanguage: "en"}

func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{AppKey: "test-app", AppSecret: sdkgo.NewSecretString("test-secret")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}
func TestTranslateTextSignsFreshPostAndReadsTranslation(t *testing.T) {
	salts := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
		require.Empty(t, request.Header.Get("Authorization"))
		require.NoError(t, request.ParseForm())
		form := request.PostForm
		require.Equal(t, testInput.Text, form.Get("q"))
		require.Equal(t, "zh-CHS", form.Get("from"))
		require.Equal(t, "en", form.Get("to"))
		require.Equal(t, "v3", form.Get("signType"))
		require.Equal(t, "test-app", form.Get("appKey"))
		salt := form.Get("salt")
		require.Len(t, salt, 32)
		require.False(t, salts[salt])
		salts[salt] = true
		_, err := strconv.ParseInt(form.Get("curtime"), 10, 64)
		require.NoError(t, err)
		require.Equal(t, signTranslation("test-app", testInput.Text, salt, form.Get("curtime"), "test-secret"), form.Get("sign"))
		_, err = io.WriteString(writer, `{"errorCode":"0","translation":["Hello, world"]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	for range 2 {
		result, err := sdkgo.RunQuery(queryContext{}, client.TranslateText(), testConnection, testInput)
		require.NoError(t, err)
		require.Equal(t, TranslateTextBranchTranslated, result.Branch)
		require.Equal(t, []string{"Hello, world"}, result.Value.Translation)
	}
	require.Len(t, salts, 2)
}
func TestTranslationSignatureUsesUnicodeCharacterCount(t *testing.T) {
	text := "你好世界测试翻译文本甲乙丙丁戊己庚辛壬癸子丑寅卯"
	require.Greater(t, utf8.RuneCountInString(text), 20)
	characters := []rune(text)
	payload := "app" + string(characters[:10]) + strconv.Itoa(len(characters)) + string(characters[len(characters)-10:]) + "salt" + "123" + "secret"
	digest := sha256.Sum256([]byte(payload))
	require.Equal(t, hex.EncodeToString(digest[:]), signTranslation("app", text, "salt", "123", "secret"))
	require.Equal(t, "6fbee10ee91061a6cfbdb18887e4991fbe3994308f6661cc71b58f06fc3a2d7c", signTranslation("app", "hello", "salt", "123", "secret"))
}
func TestTranslateTextClassifiesProviderOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"success", 200, `{"errorCode":"0","translation":["Hello"]}`, TranslateTextBranchTranslated, false},
		{"invalid key", 200, `{"errorCode":"202"}`, TranslateTextBranchProviderRejected, false},
		{"insufficient balance", 200, `{"errorCode":"401"}`, TranslateTextBranchProviderRejected, false},
		{"replay rejected", 200, `{"errorCode":"207"}`, TranslateTextBranchProviderRejected, false},
		{"rate code", 200, `{"errorCode":"411"}`, "", true},
		{"server code", 200, `{"errorCode":"303"}`, "", true},
		{"rate HTTP", 429, `{}`, "", true},
		{"server HTTP", 503, `{}`, "", true},
		{"missing code", 200, `{"translation":["Hello"]}`, TranslateTextBranchInvalidResponse, false},
		{"empty translation", 200, `{"errorCode":"0","translation":[]}`, TranslateTextBranchInvalidResponse, false},
		{"malformed", 200, `{"errorCode":`, TranslateTextBranchInvalidResponse, false},
		{"trailing", 200, `{"errorCode":"0","translation":["Hello"]}{}`, TranslateTextBranchInvalidResponse, false},
		{"unauthorized HTTP", 401, `{}`, TranslateTextBranchProviderRejected, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).TranslateText(), testConnection, testInput)
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
func TestTranslateTextBoundsAndRejectsInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"errorCode":"0","translation":["Hello"]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).TranslateText(), testConnection, testInput)
	require.NoError(t, err)
	require.Equal(t, TranslateTextBranchInvalidResponse, result.Branch)
	for _, input := range []TranslateTextInput{{Text: "", SourceLanguage: "en", TargetLanguage: "zh-CHS"}, {Text: "hello", SourceLanguage: "en", TargetLanguage: "auto"}, {Text: "hello", SourceLanguage: "bad/unsafe", TargetLanguage: "en"}} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).TranslateText(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, TranslateTextBranchDefect, result.Branch)
	}
	_, err = New(Config{BaseURL: "https://openapi.youdao.com/api"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
}
