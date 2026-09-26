// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/connectors/google/gemini/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAPIKey       = "AIzaSENTINEL-test-key_0123456789"
	providerSentinel = "SENTINEL-PROVIDER-BODY"
	contentSentinel  = "SENTINEL-CANDIDATE-CONTENT"
)

var geminiConnection = sdkgo.ConnectionRef{Provider: "google", Name: "gemini-default"}

type capturedRequest struct {
	Method string
	Path   string
	Query  string
	URL    string
	Header http.Header
	Body   map[string]any
}

// fakeGemini is a local Gemini API that records every request and replies with the next handler.
type fakeGemini struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []capturedRequest
	reply    func(response http.ResponseWriter, request *http.Request)
}

func newFakeGemini(t *testing.T, reply func(response http.ResponseWriter, request *http.Request)) *fakeGemini {
	t.Helper()
	fake := &fakeGemini{reply: reply}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		captured := capturedRequest{
			Method: request.Method, Path: request.URL.Path, Query: request.URL.RawQuery,
			URL: request.URL.String(), Header: request.Header.Clone(),
		}
		contents, err := io.ReadAll(request.Body)
		if err == nil && len(contents) > 0 {
			_ = json.Unmarshal(contents, &captured.Body)
		}
		fake.mutex.Lock()
		fake.requests = append(fake.requests, captured)
		fake.mutex.Unlock()
		fake.reply(response, request)
	}))
	t.Cleanup(fake.Close)
	return fake
}

func (fake *fakeGemini) recorded() []capturedRequest {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]capturedRequest(nil), fake.requests...)
}

func replyJSON(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(status)
		_, _ = response.Write([]byte(body))
	}
}

func stopResponse(text string) string {
	return `{"candidates":[{"content":{"role":"model","parts":[{"text":` + quoteJSON(text) + `}]},"finishReason":"STOP","index":0}],` +
		`"usageMetadata":{"promptTokenCount":4,"candidatesTokenCount":2,"totalTokenCount":6},"modelVersion":"gemini-2.5-flash","responseId":"resp-stop"}`
}

func quoteJSON(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func newTestClient(t *testing.T, endpoint string, options ...gemini.Option) *gemini.Client {
	t.Helper()
	return newTestClientWithConfig(t, gemini.Config{Endpoint: endpoint}, options...)
}

func newTestClientWithConfig(t *testing.T, config gemini.Config, options ...gemini.Option) *gemini.Client {
	t.Helper()
	client, err := gemini.New(config, sdkgo.StaticCredentialProvider[gemini.Credentials]{
		geminiConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	}, options...)
	require.NoError(t, err)
	return client
}

func runGenerate(t *testing.T, client *gemini.Client, input gemini.GenerateContentRequest) (gemini.GenerateContentResult, error) {
	t.Helper()
	return sdkgo.RunQuery(
		testsupport.NewDexContext("flow-"+t.Name(), "step-"+t.Name()), client.GenerateContent(), geminiConnection, input,
	)
}

func userPrompt(text string) []gemini.Content {
	return []gemini.Content{{Role: "user", Parts: []gemini.Part{{Text: text}}}}
}

func pointer[T any](value T) *T { return &value }

func TestGenerateContentSendsDocumentedRequestAndReturnsText(t *testing.T) {
	provider := newFakeGemini(t, replyJSON(http.StatusOK, `{
      "candidates":[{"content":{"role":"model","parts":[
        {"text":"planning the answer","thought":true},
        {"text":"{\"headline\":"},{"text":"\"Launch\"}"}
      ]},"finishReason":"STOP","index":0}],
      "promptFeedback":{"safetyRatings":[]},
      "usageMetadata":{"promptTokenCount":21,"cachedContentTokenCount":3,"candidatesTokenCount":7,"thoughtsTokenCount":11,"totalTokenCount":39},
      "modelVersion":"gemini-2.5-flash","responseId":"resp-123"
    }`))
	client := newTestClient(t, provider.URL+"/v1beta")
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"headline": map[string]any{"type": "string"}},
		"required":   []any{"headline"},
	}
	result, err := runGenerate(t, client, gemini.GenerateContentRequest{
		Model:             "gemini-2.5-flash",
		SystemInstruction: "Write a newsletter headline.",
		Contents: []gemini.Content{
			{Role: "user", Parts: []gemini.Part{{Text: "Summarize the launch."}}},
			{Role: "model", Parts: []gemini.Part{{Text: "Which launch?"}}},
			{Role: "user", Parts: []gemini.Part{{Text: "The connector launch."}, {Text: "Keep it short."}}},
		},
		ResponseJSONSchema: schema,
		Temperature:        pointer(0.2),
		MaxOutputTokens:    256,
		ThinkingBudget:     pointer(0),
	})
	require.NoError(t, err)
	require.Equal(t, gemini.GenerateContentBranchGenerated, result.Branch)
	require.Nil(t, result.Failure)
	require.Equal(t, gemini.GenerateContentResponse{
		ResponseID: "resp-123", ModelVersion: "gemini-2.5-flash", Text: `{"headline":"Launch"}`, FinishReason: "STOP",
		Usage: gemini.Usage{PromptTokens: 21, CandidateTokens: 7, ThoughtsTokens: 11, CachedContentTokens: 3, TotalTokens: 39},
	}, result.Value)
	require.Equal(t, "gemini", result.Receipt.Provider)
	require.Equal(t, "resp-123", result.Receipt.ProviderObjectID)
	require.NotEmpty(t, result.Receipt.CallID)
	require.Empty(t, result.Receipt.IdempotencyKey, "a Query has no idempotency key")

	requests := provider.recorded()
	require.Len(t, requests, 1)
	request := requests[0]
	require.Equal(t, http.MethodPost, request.Method)
	require.Equal(t, "/v1beta/models/gemini-2.5-flash:generateContent", request.Path)
	require.Empty(t, request.Query, "the API key must never travel in the query string")
	require.NotContains(t, request.URL, testAPIKey)
	require.Equal(t, testAPIKey, request.Header.Get("x-goog-api-key"))
	require.Empty(t, request.Header.Get("Authorization"))
	require.Equal(t, "application/json", request.Header.Get("Content-Type"))

	require.Equal(t, map[string]any{"parts": []any{map[string]any{"text": "Write a newsletter headline."}}}, request.Body["systemInstruction"])
	require.Equal(t, []any{
		map[string]any{"role": "user", "parts": []any{map[string]any{"text": "Summarize the launch."}}},
		map[string]any{"role": "model", "parts": []any{map[string]any{"text": "Which launch?"}}},
		map[string]any{"role": "user", "parts": []any{map[string]any{"text": "The connector launch."}, map[string]any{"text": "Keep it short."}}},
	}, request.Body["contents"])
	generationConfig, ok := request.Body["generationConfig"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, map[string]any{
		"responseMimeType":   "application/json",
		"responseJsonSchema": schema,
		"temperature":        0.2,
		"maxOutputTokens":    float64(256),
		"thinkingConfig":     map[string]any{"thinkingBudget": float64(0)},
	}, generationConfig)
	require.NotContains(t, generationConfig, "responseSchema", "the deprecated OpenAPI schema field must not be sent")
}

func TestGenerateContentOmitsUnsetOptionsAndPreservesExplicitValues(t *testing.T) {
	for _, test := range []struct {
		name                 string
		input                gemini.GenerateContentRequest
		wantPath             string
		wantGenerationConfig map[string]any
	}{
		{
			name: "minimal request", wantPath: "/models/gemini-2.5-flash:generateContent",
			input: gemini.GenerateContentRequest{
				Model: "models/gemini-2.5-flash", Contents: []gemini.Content{{Parts: []gemini.Part{{Text: "Hello"}}}},
			},
		},
		{
			name: "explicit zero temperature and dynamic thinking", wantPath: "/models/gemini-2.5-pro:generateContent",
			input: gemini.GenerateContentRequest{
				Model: "gemini-2.5-pro", Contents: userPrompt("Hello"), Temperature: pointer(0.0), ThinkingBudget: pointer(-1),
			},
			wantGenerationConfig: map[string]any{"temperature": float64(0), "thinkingConfig": map[string]any{"thinkingBudget": float64(-1)}},
		},
		{
			name: "response MIME type without schema", wantPath: "/models/gemini-2.5-flash:generateContent",
			input: gemini.GenerateContentRequest{
				Model: "gemini-2.5-flash", Contents: userPrompt("Classify"), ResponseMIMEType: "text/x.enum",
			},
			wantGenerationConfig: map[string]any{"responseMimeType": "text/x.enum"},
		},
		{
			name: "empty schema still sends JSON mode", wantPath: "/models/gemini-2.5-flash:generateContent",
			input: gemini.GenerateContentRequest{
				Model: "gemini-2.5-flash", Contents: userPrompt("Any JSON"), ResponseJSONSchema: map[string]any{},
			},
			wantGenerationConfig: map[string]any{"responseMimeType": "application/json", "responseJsonSchema": map[string]any{}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newFakeGemini(t, replyJSON(http.StatusOK, stopResponse("ok")))
			result, err := runGenerate(t, newTestClient(t, provider.URL+"/"), test.input)
			require.NoError(t, err)
			require.Equal(t, gemini.GenerateContentBranchGenerated, result.Branch)
			request := provider.recorded()[0]
			require.Equal(t, test.wantPath, request.Path)
			require.NotContains(t, request.Body, "systemInstruction")
			if test.wantGenerationConfig == nil {
				require.NotContains(t, request.Body, "generationConfig")
			} else {
				require.Equal(t, test.wantGenerationConfig, request.Body["generationConfig"])
			}
			if test.name == "minimal request" {
				content := request.Body["contents"].([]any)[0].(map[string]any)
				require.NotContains(t, content, "role")
			}
		})
	}
}

func TestGenerateContentClassifiesSuccessfulResponses(t *testing.T) {
	candidate := func(finishReason string, parts string) string {
		return `{"candidates":[{"content":{"role":"model","parts":[` + parts + `]},"finishReason":"` + finishReason + `"}],` +
			`"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3,"totalTokenCount":8},"responseId":"resp-branch","modelVersion":"gemini-2.5-flash"}`
	}
	sentinelPart := `{"text":"` + contentSentinel + `"}`
	type expectation struct {
		name         string
		body         string
		branch       sdkgo.BranchID
		kind         sdkgo.FailureKind
		text         string
		finishReason string
		blockReason  string
	}
	tests := []expectation{
		{name: "max tokens keeps partial text", body: candidate("MAX_TOKENS", `{"text":"partial "},{"text":"answer"}`),
			branch: gemini.GenerateContentBranchTruncated, kind: sdkgo.FailureResponseTooLarge, text: "partial answer", finishReason: "MAX_TOKENS"},
		{name: "max tokens spent on thinking", body: `{"candidates":[{"content":{"role":"model"},"finishReason":"MAX_TOKENS"}],"usageMetadata":{"thoughtsTokenCount":64}}`,
			branch: gemini.GenerateContentBranchTruncated, kind: sdkgo.FailureResponseTooLarge, finishReason: "MAX_TOKENS"},
		{name: "prompt blocked for safety", body: `{"promptFeedback":{"blockReason":"SAFETY","safetyRatings":[]},"usageMetadata":{"promptTokenCount":9}}`,
			branch: gemini.GenerateContentBranchBlocked, kind: sdkgo.FailureProviderRejection, blockReason: "SAFETY"},
		{name: "prompt blocked for another reason", body: `{"promptFeedback":{"blockReason":"OTHER"}}`,
			branch: gemini.GenerateContentBranchBlocked, kind: sdkgo.FailureProviderRejection, blockReason: "OTHER"},
		{name: "no candidates", body: `{"candidates":[],"usageMetadata":{"promptTokenCount":1}}`,
			branch: gemini.GenerateContentBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "null response", body: `null`, branch: gemini.GenerateContentBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "missing finish reason", body: candidate("", sentinelPart),
			branch: gemini.GenerateContentBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "stop without text", body: candidate("STOP", `{"text":"only thoughts","thought":true}`),
			branch: gemini.GenerateContentBranchInvalidResponse, kind: sdkgo.FailureProtocol, finishReason: "STOP"},
		{name: "other finish reason", body: candidate("OTHER", sentinelPart),
			branch: gemini.GenerateContentBranchInvalidResponse, kind: sdkgo.FailureProtocol, finishReason: "OTHER"},
		{name: "malformed function call", body: candidate("MALFORMED_FUNCTION_CALL", sentinelPart),
			branch: gemini.GenerateContentBranchInvalidResponse, kind: sdkgo.FailureProtocol, finishReason: "MALFORMED_FUNCTION_CALL"},
		{name: "future finish reason", body: candidate("SOMETHING_NEW", sentinelPart),
			branch: gemini.GenerateContentBranchInvalidResponse, kind: sdkgo.FailureProtocol, finishReason: "SOMETHING_NEW"},
		{name: "unrecognized finish reason text", body: candidate("stop now", sentinelPart),
			branch: gemini.GenerateContentBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "unrecognized block reason text", body: `{"promptFeedback":{"blockReason":"blocked: ` + contentSentinel + `"}}`,
			branch: gemini.GenerateContentBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "malformed JSON", body: `{"candidates":[`, branch: gemini.GenerateContentBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "JSON array", body: `[` + candidate("STOP", sentinelPart) + `]`, branch: gemini.GenerateContentBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "trailing data", body: candidate("STOP", sentinelPart) + `{}`, branch: gemini.GenerateContentBranchInvalidResponse, kind: sdkgo.FailureProtocol},
	}
	for _, reason := range []string{"SAFETY", "RECITATION", "LANGUAGE", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY", "ESCALATION", "PUP_LIMITED_DISABLED"} {
		tests = append(tests, expectation{
			name: "candidate stopped for " + reason, body: candidate(reason, sentinelPart),
			branch: gemini.GenerateContentBranchBlocked, kind: sdkgo.FailureProviderRejection, finishReason: reason,
		})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := newFakeGemini(t, replyJSON(http.StatusOK, test.body))
			result, err := runGenerate(t, newTestClient(t, provider.URL), gemini.GenerateContentRequest{
				Model: "gemini-2.5-flash", Contents: userPrompt("Summarize"),
			})
			require.NoError(t, err, "a conclusive 2xx response never retries")
			require.Equal(t, test.branch, result.Branch)
			require.NotNil(t, result.Failure)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, "gemini", result.Failure.Provider)
			require.Equal(t, "generateContent", result.Failure.Operation)
			require.Equal(t, test.text, result.Value.Text)
			require.Equal(t, test.finishReason, result.Value.FinishReason)
			require.Equal(t, test.blockReason, result.Value.BlockReason)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), contentSentinel, "only generated and truncated results carry candidate text")
			require.Len(t, provider.recorded(), 1)
		})
	}
}

func TestGenerateContentBoundsTheResponseSize(t *testing.T) {
	body := stopResponse(strings.Repeat("x", 512))
	for _, test := range []struct {
		name     string
		limit    int64
		branch   sdkgo.BranchID
		wantKind sdkgo.FailureKind
	}{
		{name: "exactly at the limit", limit: int64(len(body)), branch: gemini.GenerateContentBranchGenerated},
		{name: "one byte over the limit", limit: int64(len(body) - 1), branch: gemini.GenerateContentBranchInvalidResponse, wantKind: sdkgo.FailureResponseTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newFakeGemini(t, replyJSON(http.StatusOK, body))
			client := newTestClientWithConfig(t, gemini.Config{Endpoint: provider.URL, MaxResponseBytes: test.limit})
			result, err := runGenerate(t, client, gemini.GenerateContentRequest{Model: "gemini-2.5-flash", Contents: userPrompt("Hi")})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.wantKind == "" {
				require.Nil(t, result.Failure)
				require.Len(t, result.Value.Text, 512)
				return
			}
			require.Equal(t, test.wantKind, result.Failure.Kind)
			require.Empty(t, result.Value.Text, "an oversized response is never silently truncated")
		})
	}
}

func googleErrorBody(code int, status string, details ...string) string {
	return fmt.Sprintf(`{"error":{"code":%d,"message":"%s echoed key %s","status":"%s","details":[%s]}}`,
		code, providerSentinel, testAPIKey, status, strings.Join(details, ","))
}

func TestGenerateContentClassifiesConclusiveProviderRejections(t *testing.T) {
	apiKeyInvalid := `{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_INVALID","domain":"googleapis.com"}`
	for _, test := range []struct {
		name        string
		status      int
		body        string
		wantKind    sdkgo.FailureKind
		wantMessage string
	}{
		{name: "invalid argument", status: 400, body: googleErrorBody(400, "INVALID_ARGUMENT"), wantKind: sdkgo.FailureProviderRejection, wantMessage: "provider returned HTTP 400 INVALID_ARGUMENT"},
		{name: "invalid API key", status: 400, body: googleErrorBody(400, "INVALID_ARGUMENT", apiKeyInvalid), wantKind: sdkgo.FailureAuthentication, wantMessage: "provider returned HTTP 400 INVALID_ARGUMENT"},
		{name: "failed precondition", status: 400, body: googleErrorBody(400, "FAILED_PRECONDITION"), wantKind: sdkgo.FailureProviderRejection, wantMessage: "provider returned HTTP 400 FAILED_PRECONDITION"},
		{name: "unauthenticated", status: 401, body: googleErrorBody(401, "UNAUTHENTICATED"), wantKind: sdkgo.FailureAuthentication, wantMessage: "provider returned HTTP 401 UNAUTHENTICATED"},
		{name: "prepay credits depleted", status: 402, body: `{}`, wantKind: sdkgo.FailureProviderRejection, wantMessage: "provider returned HTTP 402"},
		{name: "permission denied", status: 403, body: googleErrorBody(403, "PERMISSION_DENIED"), wantKind: sdkgo.FailureAuthorization, wantMessage: "provider returned HTTP 403 PERMISSION_DENIED"},
		{name: "unknown model", status: 404, body: googleErrorBody(404, "NOT_FOUND"), wantKind: sdkgo.FailureNotFound, wantMessage: "provider returned HTTP 404 NOT_FOUND"},
		{name: "unimplemented", status: 501, body: googleErrorBody(501, "UNIMPLEMENTED"), wantKind: sdkgo.FailureProviderRejection, wantMessage: "provider returned HTTP 501 UNIMPLEMENTED"},
		{name: "non-JSON body", status: 400, body: "<html>" + providerSentinel + "</html>", wantKind: sdkgo.FailureProviderRejection, wantMessage: "provider returned HTTP 400"},
		{name: "unrecognized status name", status: 400, body: googleErrorBody(400, "bad "+providerSentinel), wantKind: sdkgo.FailureProviderRejection, wantMessage: "provider returned HTTP 400"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newFakeGemini(t, func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("X-Goog-Request-Id", "req-rejected")
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(test.body))
			})
			result, err := runGenerate(t, newTestClient(t, provider.URL), gemini.GenerateContentRequest{Model: "gemini-unknown", Contents: userPrompt("Hi")})
			require.NoError(t, err, "a conclusive rejection must not retry")
			require.Equal(t, gemini.GenerateContentBranchProviderRejected, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
			require.Equal(t, test.wantMessage, result.Failure.Message)
			require.Equal(t, "req-rejected", result.Receipt.ProviderRequestID)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), providerSentinel)
			require.NotContains(t, string(encoded), testAPIKey)
			require.Len(t, provider.recorded(), 1)
		})
	}
}

func TestGenerateContentDoesNotFollowRedirects(t *testing.T) {
	redirectTarget := newFakeGemini(t, replyJSON(http.StatusOK, stopResponse("redirected")))
	provider := newFakeGemini(t, func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, redirectTarget.URL+request.URL.Path, http.StatusTemporaryRedirect)
	})
	result, err := runGenerate(t, newTestClient(t, provider.URL), gemini.GenerateContentRequest{Model: "gemini-2.5-flash", Contents: userPrompt("Hi")})
	require.NoError(t, err)
	require.Equal(t, gemini.GenerateContentBranchProviderRejected, result.Branch)
	require.Equal(t, "provider returned HTTP 307", result.Failure.Message)
	require.Empty(t, redirectTarget.recorded(), "the API key header must never be forwarded to a redirect target")
}

func TestGenerateContentRetriesTransientFailuresWithProviderDelay(t *testing.T) {
	retryInfo := func(delay string) string {
		return `{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"` + delay + `"}`
	}
	quotaFailure := `{"@type":"type.googleapis.com/google.rpc.QuotaFailure","violations":[{"quotaMetric":"` + providerSentinel + `"}]}`
	for _, test := range []struct {
		name       string
		status     int
		header     map[string]string
		body       string
		wantKind   sdkgo.FailureKind
		wantDelay  time.Duration
		wantStatus string
	}{
		{name: "rate limit with RetryInfo", status: 429, body: googleErrorBody(429, "RESOURCE_EXHAUSTED", quotaFailure, retryInfo("37s")),
			wantKind: sdkgo.FailureRateLimit, wantDelay: 37 * time.Second, wantStatus: "provider returned HTTP 429 RESOURCE_EXHAUSTED"},
		{name: "fractional RetryInfo", status: 429, body: googleErrorBody(429, "RESOURCE_EXHAUSTED", retryInfo("1.5s")),
			wantKind: sdkgo.FailureRateLimit, wantDelay: 1500 * time.Millisecond, wantStatus: "provider returned HTTP 429 RESOURCE_EXHAUSTED"},
		{name: "RetryInfo wins over Retry-After", status: 429, header: map[string]string{"Retry-After": "60"}, body: googleErrorBody(429, "RESOURCE_EXHAUSTED", retryInfo("5s")),
			wantKind: sdkgo.FailureRateLimit, wantDelay: 5 * time.Second, wantStatus: "provider returned HTTP 429 RESOURCE_EXHAUSTED"},
		{name: "Retry-After seconds", status: 429, header: map[string]string{"Retry-After": "12"}, body: `{}`,
			wantKind: sdkgo.FailureRateLimit, wantDelay: 12 * time.Second, wantStatus: "provider returned HTTP 429"},
		{name: "very long RetryInfo is capped", status: 429, body: googleErrorBody(429, "RESOURCE_EXHAUSTED", retryInfo("86400s")),
			wantKind: sdkgo.FailureRateLimit, wantDelay: time.Hour, wantStatus: "provider returned HTTP 429 RESOURCE_EXHAUSTED"},
		{name: "invalid RetryInfo uses Step policy", status: 429, body: googleErrorBody(429, "RESOURCE_EXHAUSTED", retryInfo("soon")),
			wantKind: sdkgo.FailureRateLimit, wantStatus: "provider returned HTTP 429 RESOURCE_EXHAUSTED"},
		{name: "unavailable", status: 503, body: googleErrorBody(503, "UNAVAILABLE"),
			wantKind: sdkgo.FailureAvailability, wantStatus: "provider returned HTTP 503 UNAVAILABLE"},
		{name: "unavailable with RetryInfo", status: 503, body: googleErrorBody(503, "UNAVAILABLE", retryInfo("2s")),
			wantKind: sdkgo.FailureAvailability, wantDelay: 2 * time.Second, wantStatus: "provider returned HTTP 503 UNAVAILABLE"},
		{name: "internal", status: 500, body: googleErrorBody(500, "INTERNAL"),
			wantKind: sdkgo.FailureAvailability, wantStatus: "provider returned HTTP 500 INTERNAL"},
		{name: "bad gateway", status: 502, body: `<html>` + providerSentinel + `</html>`,
			wantKind: sdkgo.FailureAvailability, wantStatus: "provider returned HTTP 502"},
		{name: "deadline exceeded", status: 504, body: googleErrorBody(504, "DEADLINE_EXCEEDED"),
			wantKind: sdkgo.FailureAvailability, wantStatus: "provider returned HTTP 504 DEADLINE_EXCEEDED"},
		{name: "request timeout", status: 408, body: `{}`,
			wantKind: sdkgo.FailureAvailability, wantStatus: "provider returned HTTP 408"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newFakeGemini(t, func(response http.ResponseWriter, _ *http.Request) {
				for name, value := range test.header {
					response.Header().Set(name, value)
				}
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(test.body))
			})
			_, err := runGenerate(t, newTestClient(t, provider.URL), gemini.GenerateContentRequest{Model: "gemini-2.5-flash", Contents: userPrompt("Hi")})
			require.Error(t, err)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.wantKind, retry.Failure.Kind)
			require.Equal(t, test.wantStatus, retry.Failure.Message)
			var retryAfter *dex.RetryAfterError
			if test.wantDelay == 0 {
				require.False(t, errors.As(err, &retryAfter), "no provider delay means the Step retry policy decides")
			} else {
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.wantDelay, retryAfter.After)
			}
			require.NotContains(t, err.Error(), providerSentinel)
			require.NotContains(t, err.Error(), testAPIKey)
		})
	}
}

func TestGenerateContentRetriesTransportFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		reply   func(http.ResponseWriter, *http.Request)
		message string
	}{
		{
			name: "connection closed before a response",
			reply: func(response http.ResponseWriter, _ *http.Request) {
				connection, _, err := response.(http.Hijacker).Hijack()
				if err == nil {
					_ = connection.Close()
				}
			},
			message: "provider request failed before a response was received",
		},
		{
			name: "connection closed mid-body",
			reply: func(response http.ResponseWriter, _ *http.Request) {
				connection, buffer, err := response.(http.Hijacker).Hijack()
				if err != nil {
					return
				}
				_, _ = buffer.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 4096\r\n\r\n{\"candidates\":[")
				_ = buffer.Flush()
				_ = connection.Close()
			},
			message: "provider response could not be read",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newFakeGemini(t, test.reply)
			_, err := runGenerate(t, newTestClient(t, provider.URL), gemini.GenerateContentRequest{Model: "gemini-2.5-flash", Contents: userPrompt("Hi")})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
			require.Equal(t, test.message, retry.Failure.Message)
		})
	}
}

func TestGenerateContentRejectsInvalidInputBeforeDispatch(t *testing.T) {
	valid := func() gemini.GenerateContentRequest {
		return gemini.GenerateContentRequest{Model: "gemini-2.5-flash", Contents: userPrompt("Hi")}
	}
	for _, test := range []struct {
		name   string
		mutate func(*gemini.GenerateContentRequest)
	}{
		{name: "empty model", mutate: func(input *gemini.GenerateContentRequest) { input.Model = "" }},
		{name: "model path traversal", mutate: func(input *gemini.GenerateContentRequest) { input.Model = "../files/x" }},
		{name: "model with method suffix", mutate: func(input *gemini.GenerateContentRequest) { input.Model = "gemini-2.5-flash:countTokens" }},
		{name: "model with query", mutate: func(input *gemini.GenerateContentRequest) { input.Model = "gemini-2.5-flash?key=x" }},
		{name: "tuned model resource", mutate: func(input *gemini.GenerateContentRequest) { input.Model = "tunedModels/custom" }},
		{name: "no contents", mutate: func(input *gemini.GenerateContentRequest) { input.Contents = nil }},
		{name: "content without parts", mutate: func(input *gemini.GenerateContentRequest) { input.Contents = []gemini.Content{{Role: "user"}} }},
		{name: "empty text part", mutate: func(input *gemini.GenerateContentRequest) {
			input.Contents = []gemini.Content{{Role: "user", Parts: []gemini.Part{{}}}}
		}},
		{name: "unknown role", mutate: func(input *gemini.GenerateContentRequest) { input.Contents[0].Role = "system" }},
		{name: "temperature below range", mutate: func(input *gemini.GenerateContentRequest) { input.Temperature = pointer(-0.1) }},
		{name: "temperature above range", mutate: func(input *gemini.GenerateContentRequest) { input.Temperature = pointer(2.01) }},
		{name: "negative max output tokens", mutate: func(input *gemini.GenerateContentRequest) { input.MaxOutputTokens = -1 }},
		{name: "thinking budget below dynamic", mutate: func(input *gemini.GenerateContentRequest) { input.ThinkingBudget = pointer(-2) }},
		{name: "schema with non-JSON MIME type", mutate: func(input *gemini.GenerateContentRequest) {
			input.ResponseMIMEType = "text/plain"
			input.ResponseJSONSchema = map[string]any{"type": "string"}
		}},
		{name: "schema that cannot serialize", mutate: func(input *gemini.GenerateContentRequest) {
			input.ResponseJSONSchema = map[string]any{"type": func() {}}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newFakeGemini(t, replyJSON(http.StatusOK, stopResponse("unexpected")))
			input := valid()
			test.mutate(&input)
			result, err := runGenerate(t, newTestClient(t, provider.URL), input)
			require.NoError(t, err)
			require.Equal(t, gemini.GenerateContentBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
			require.Empty(t, provider.recorded(), "invalid input must not reach the provider")
		})
	}
	t.Run("NaN temperature", func(t *testing.T) {
		provider := newFakeGemini(t, replyJSON(http.StatusOK, stopResponse("unexpected")))
		input := valid()
		nan := 0.0
		nan = nan / nan
		input.Temperature = &nan
		result, err := runGenerate(t, newTestClient(t, provider.URL), input)
		require.NoError(t, err)
		require.Equal(t, gemini.GenerateContentBranchDefect, result.Branch)
		require.Empty(t, provider.recorded())
	})
}

func TestGenerateContentCredentialFailuresUseDefectWithoutDispatch(t *testing.T) {
	for _, test := range []struct {
		name        string
		credentials sdkgo.StaticCredentialProvider[gemini.Credentials]
		message     string
	}{
		{name: "connection is not configured", credentials: sdkgo.StaticCredentialProvider[gemini.Credentials]{}, message: "connection credentials are unavailable"},
		{name: "empty API key", credentials: sdkgo.StaticCredentialProvider[gemini.Credentials]{
			geminiConnection: {APIKey: sdkgo.NewSecretString("")},
		}, message: "connection credentials are invalid"},
		{name: "API key with header injection", credentials: sdkgo.StaticCredentialProvider[gemini.Credentials]{
			geminiConnection: {APIKey: sdkgo.NewSecretString("AIzaKEY\r\nX-Injected: yes")},
		}, message: "connection credentials are invalid"},
		{name: "API key with a space", credentials: sdkgo.StaticCredentialProvider[gemini.Credentials]{
			geminiConnection: {APIKey: sdkgo.NewSecretString("AIza KEY")},
		}, message: "connection credentials are invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newFakeGemini(t, replyJSON(http.StatusOK, stopResponse("unexpected")))
			client, err := gemini.New(gemini.Config{Endpoint: provider.URL}, test.credentials)
			require.NoError(t, err)
			result, err := runGenerate(t, client, gemini.GenerateContentRequest{Model: "gemini-2.5-flash", Contents: userPrompt("Hi")})
			require.NoError(t, err)
			require.Equal(t, gemini.GenerateContentBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			require.Equal(t, test.message, result.Failure.Message)
			require.Empty(t, provider.recorded())
		})
	}
}

func TestConnectorValuesNeverRenderTheAPIKey(t *testing.T) {
	credentials := gemini.Credentials{APIKey: sdkgo.NewSecretString(testAPIKey)}
	client := newTestClient(t, "https://generativelanguage.googleapis.com/v1beta")
	connection, err := gemini.NewConnection(client, geminiConnection)
	require.NoError(t, err)
	for _, value := range []any{credentials, &credentials, client, connection} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
			require.NotContains(t, fmt.Sprintf(format, value), testAPIKey, format)
		}
	}
	_, err = json.Marshal(credentials)
	require.Error(t, err, "credentials cannot be serialized")
	_, err = json.Marshal(connection)
	require.ErrorContains(t, err, "cannot be serialized")
}

func TestNewValidatesEndpointAndOptions(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[gemini.Credentials]{}
	for _, test := range []struct {
		name     string
		endpoint string
		wantErr  string
	}{
		{name: "default endpoint", endpoint: ""},
		{name: "HTTPS endpoint", endpoint: "https://generativelanguage.googleapis.com/v1beta/"},
		{name: "loopback IPv4", endpoint: "http://127.0.0.1:9999/v1beta"},
		{name: "loopback IPv6", endpoint: "http://[::1]:9999"},
		{name: "localhost", endpoint: "http://localhost:9999"},
		{name: "plain HTTP", endpoint: "http://generativelanguage.googleapis.com/v1beta", wantErr: "must use HTTPS"},
		{name: "other scheme", endpoint: "ftp://generativelanguage.googleapis.com", wantErr: "must use HTTPS"},
		{name: "relative", endpoint: "/v1beta", wantErr: "absolute URL"},
		{name: "query", endpoint: "https://generativelanguage.googleapis.com/v1beta?key=x", wantErr: "cannot contain"},
		{name: "user info", endpoint: "https://user:secret@generativelanguage.googleapis.com", wantErr: "cannot contain"},
		{name: "fragment", endpoint: "https://generativelanguage.googleapis.com/v1beta#x", wantErr: "cannot contain"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := gemini.New(gemini.Config{Endpoint: test.endpoint}, credentials)
			if test.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.wantErr)
			}
		})
	}
	_, err := gemini.New(gemini.Config{MaxResponseBytes: -1}, credentials)
	require.ErrorContains(t, err, "cannot be negative")
	_, err = gemini.New(gemini.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = gemini.New(gemini.Config{}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
	require.Equal(t, gemini.Config{Endpoint: "https://generativelanguage.googleapis.com/v1beta", MaxResponseBytes: 8 << 20}, gemini.DefaultConfig())
}

type countingTransport struct {
	mutex    sync.Mutex
	requests int
}

func (transport *countingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mutex.Lock()
	transport.requests++
	transport.mutex.Unlock()
	return http.DefaultTransport.RoundTrip(request)
}

func TestWithHTTPClientUsesTheCallerTransportWithoutMutatingIt(t *testing.T) {
	provider := newFakeGemini(t, replyJSON(http.StatusOK, stopResponse("custom transport")))
	transport := &countingTransport{}
	callerClient := &http.Client{Transport: transport}
	client := newTestClient(t, provider.URL, gemini.WithHTTPClient(callerClient))
	result, err := runGenerate(t, client, gemini.GenerateContentRequest{Model: "gemini-2.5-flash", Contents: userPrompt("Hi")})
	require.NoError(t, err)
	require.Equal(t, gemini.GenerateContentBranchGenerated, result.Branch)
	require.Equal(t, 1, transport.requests)
	require.Nil(t, callerClient.CheckRedirect, "the connector configures its own copy")
	require.Zero(t, callerClient.Timeout)
}

func TestNewLocalConnectionReloadsCredentialsForEveryCall(t *testing.T) {
	provider := newFakeGemini(t, replyJSON(http.StatusOK, stopResponse("local")))
	path := filepath.Join(t.TempDir(), "connections.json")
	writeConnection := func(apiKey string) {
		contents, err := json.Marshal(map[string]any{
			"schemaVersion": localconfig.SchemaVersion,
			"connections": []any{map[string]any{
				"connectorId": gemini.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/google/gemini",
				"moduleVersion": "v0.1.0", "provider": "google", "connectionName": "gemini-local",
				"configuration": map[string]any{"endpoint": provider.URL},
				"credentials":   map[string]any{"api_key": apiKey},
			}},
		})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, contents, 0o600))
	}
	writeConnection("AIzaFIRST-local-key")
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	connection, err := gemini.NewLocalConnection(store, "gemini-local")
	require.NoError(t, err)
	step := gemini.NewGenerateContentStep(gemini.GenerateContentStepConfig[string]{
		StepType: "GenerateLocal", ConnectionName: "gemini-local", Annotations: geminiAnnotations(), Connection: connection,
		MapToOperationInput: func(prompt string) gemini.GenerateContentRequest {
			return gemini.GenerateContentRequest{Model: "gemini-2.5-flash", Contents: userPrompt(prompt)}
		},
		Generated: sdkgo.GoTo(generatedTarget{}),
	})
	for index, key := range []string{"AIzaFIRST-local-key", "AIzaSECOND-local-key"} {
		if index > 0 {
			writeConnection(key)
		}
		decision, err := step.Execute(testsupport.NewDexContext("local-flow", fmt.Sprintf("local-step-%d", index)), "Hi")
		require.NoError(t, err)
		require.NotNil(t, decision)
		requests := provider.recorded()
		require.Len(t, requests, index+1)
		require.Equal(t, key, requests[index].Header.Get("x-goog-api-key"), "credential replacement is visible without a restart")
	}
}
