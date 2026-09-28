// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qwen_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/alibaba/qwen"
	"github.com/superdurable/dex-connectors-library/connectors/alibaba/qwen/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat/openaichattest"
)

const testAPIKey = "sk-qwen-connector-test-key-0123456789"

var testConnection = sdkgo.ConnectionRef{Provider: "alibaba", Name: "qwen-test"}

// qwenDialect matches chatProfile: streamed replies, the 400 Arrearage billing error, and the 400 moderation error.
var qwenDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "qwen3.7-plus", AlternateModel: "qwen3.8-flash", IsStreaming: true,
	QuotaExhaustedStatusCode: http.StatusBadRequest, QuotaExhaustedErrorToken: "Arrearage",
	ContentPolicyErrorToken: "data_inspection_failed",
})

func TestGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureTwo := 2.0
	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
		Dialect:  qwenDialect,
		NewQuery: newQwenQuery,
		LocallyRejectedRequests: []llmtest.NamedTextGenerationRequest{
			{Name: "temperature 2, outside the half-open [0, 2) range", Request: llm.TextGenerationRequest{Temperature: &temperatureTwo}},
			{Name: "reasoning effort on qwen3.7-plus, which documents none", Request: llm.TextGenerationRequest{
				ReasoningEffort: llm.ReasoningEffortHigh,
			}},
			{Name: "high reasoning effort, which Qwen3.8 silently maps to xhigh", Request: llm.TextGenerationRequest{
				Model: "qwen3.8-max", ReasoningEffort: llm.ReasoningEffortHigh,
			}},
			{Name: "max reasoning effort on qwen3.8-flash", Request: llm.TextGenerationRequest{
				Model: "qwen3.8-flash", ReasoningEffort: llm.ReasoningEffortMax,
			}},
			{Name: "reasoning effort none on the thinking-only qwen3.8-2.4t-a95b", Request: llm.TextGenerationRequest{
				Model: "qwen3.8-2.4t-a95b", ReasoningEffort: llm.ReasoningEffortNone,
			}},
		},
	})
}

// TestGenerateTextSendsTheDocumentedChatCompletionsRequest pins the body fields Model Studio documents for Chat Completions.
func TestGenerateTextSendsTheDocumentedChatCompletionsRequest(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, qwenDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(qwenDialect.GeneratedReply(llmtest.GeneratedReply{
		Text: `{"answer":"TCP is reliable."}`, ServedModel: "qwen3.7-plus", ResponseID: "chatcmpl-qwen-1",
		Usage: llm.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44},
	}))
	temperature := 0.4
	result := runGenerateText(t, newQwenClient(t, provider.BaseURL(), ""), qwen.GenerateTextRequest{
		Instructions: "Answer in one sentence.",
		Messages:     []llm.Message{{Role: llm.MessageRoleUser, Text: "How does TCP differ from UDP?"}},
		StructuredOutput: &llm.StructuredOutput{Name: "answer", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
			"required": []any{"answer"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature,
	})

	require.Equal(t, qwen.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, `{"answer":"TCP is reliable."}`, result.Value.Text)
	require.Equal(t, "qwen3.7-plus", result.Value.RequestedModel)
	require.Equal(t, int64(12), result.Value.Usage.ReasoningTokens)
	require.Equal(t, "qwen", result.Receipt.Provider)
	requests := provider.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].Method)
	require.Equal(t, "/chat/completions", requests[0].Path)
	require.Equal(t, "text/event-stream", requests[0].Header.Get("Accept"))
	require.True(t, requests[0].HasCredentialInSlot, "the key travels as an Authorization bearer token")
	var body struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(requests[0].Body, &body))
	require.Len(t, body.Messages, 2)
	require.Equal(t, "system", body.Messages[0].Role, "instructions go first, as the system message Model Studio documents")
	require.True(t, strings.HasPrefix(body.Messages[0].Content, "Answer in one sentence.\n\n"))
	require.Contains(t, body.Messages[0].Content, "JSON", "json_object mode requires the word JSON in the messages")
	require.Contains(t, body.Messages[0].Content, `"answer"`, "the instruction carries the schema")
	require.Equal(t, "user", body.Messages[1].Role)
	require.Equal(t, "How does TCP differ from UDP?", body.Messages[1].Content)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(requests[0].Body, &fields))
	delete(fields, "messages")
	require.Equal(t, map[string]any{
		"model":                 "qwen3.7-plus",
		"max_completion_tokens": float64(2048),
		"temperature":           0.4,
		"response_format":       map[string]any{"type": "json_object"},
		"stream":                true,
		"stream_options":        map[string]any{"include_usage": true},
	}, fields)
}

// TestGenerateTextSelectsTheDocumentedTokenLimitField sends max_completion_tokens only where Model Studio documents it.
func TestGenerateTextSelectsTheDocumentedTokenLimitField(t *testing.T) {
	for _, testCase := range []struct{ model, field string }{
		{"qwen3.7-plus", "max_completion_tokens"},
		{"qwen3.7-plus-2026-05-26", "max_completion_tokens"},
		{"qwen3.5-flash-2026-02-23", "max_completion_tokens"},
		{"qwen3.6-plus", "max_completion_tokens"},
		{"qwen3.7-max-preview", "max_completion_tokens"},
		{"qwen3.7-max-2026-05-20", "max_completion_tokens"},
		{"qwen3.8-max", "max_completion_tokens"},
		{"qwen3.8-max-0902", "max_completion_tokens"},
		{"qwen3.8-flash", "max_completion_tokens"},
		{"qwen-plus", "max_tokens"},
		{"qwen3.6-max-preview", "max_tokens"},
		{"qwen3.8-27b", "max_tokens"},
		{"qwen3.5-omni-plus", "max_tokens"},
		{"qwen3.5-397b-a17b", "max_tokens"},
		{"deepseek-v4-pro", "max_tokens"},
	} {
		t.Run(testCase.model, func(t *testing.T) {
			body := generateAndReadRequestBody(t, qwen.GenerateTextRequest{
				Model: testCase.model, Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: "Hello"}}, MaxOutputTokens: 512,
			})
			require.Equal(t, float64(512), body[testCase.field])
			require.Len(t, body, 5, "model, messages, one token-limit field, stream, and stream_options")
		})
	}
}

// TestGenerateTextMapsQwen38ReasoningEfforts sends only the efforts Qwen3.8 documents as native values.
func TestGenerateTextMapsQwen38ReasoningEfforts(t *testing.T) {
	for _, testCase := range []struct {
		model  string
		effort llm.ReasoningEffort
		wire   string
	}{
		{"qwen3.8-flash", llm.ReasoningEffortNone, "none"},
		{"qwen3.8-flash", llm.ReasoningEffortLow, "low"},
		{"qwen3.8-max", llm.ReasoningEffortMedium, "medium"},
		{"qwen3.8-max-0902", llm.ReasoningEffortExtraHigh, "xhigh"},
		{"qwen3.8-27b", llm.ReasoningEffortNone, "none"},
		{"qwen3.8-2.4t-a95b", llm.ReasoningEffortLow, "low"},
	} {
		t.Run(testCase.model+"/"+string(testCase.effort), func(t *testing.T) {
			body := generateAndReadRequestBody(t, qwen.GenerateTextRequest{
				Model: testCase.model, Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: "Plan the migration."}},
				ReasoningEffort: testCase.effort,
			})
			require.Equal(t, testCase.wire, body["reasoning_effort"])
		})
	}
}

// TestContentModerationSelectsBlocked covers the native token and an error object inside a stream.
func TestContentModerationSelectsBlocked(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		reply llmtest.FakeReply
	}{
		{"native DataInspectionFailed code", llmtest.FakeReply{
			StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": {"application/json"}},
			Body: `{"error":{"message":"Input data may contain inappropriate content.","type":"DataInspectionFailed","param":null,"code":"DataInspectionFailed"}}`,
		}},
		{"error object inside the stream", llmtest.FakeReply{
			Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: `data: {"id":"chatcmpl-qwen-2","object":"chat.completion.chunk","model":"qwen3.7-plus",` +
				`"choices":[{"index":0,"delta":{"role":"assistant","content":"Partial"},"finish_reason":null}]}` + "\n\n" +
				`data: {"error":{"message":"Output data may contain inappropriate content.","type":"data_inspection_failed","param":null,"code":"data_inspection_failed"}}` + "\n\n",
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, qwenDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(testCase.reply)
			result := runGenerateText(t, newQwenClient(t, provider.BaseURL(), ""), userRequest("Hello"))
			require.Equal(t, qwen.GenerateTextBranchBlocked, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, llm.FinishReasonContentPolicy, result.Value.FinishReason)
			require.Empty(t, result.Value.Text)
			require.NotContains(t, result.Failure.Message, "inappropriate", "a Failure never carries provider message text")
		})
	}
}

// TestAccountAndBillingErrorsAreNotRetried keeps billing states on providerRejected and throttling on Retry.
func TestAccountAndBillingErrorsAreNotRetried(t *testing.T) {
	for _, testCase := range []struct {
		status int
		code   string
	}{
		{http.StatusBadRequest, "Arrearage"},
		{http.StatusForbidden, "AllocationQuota.FreeTierOnly"},
		{http.StatusTooManyRequests, "CommodityNotPurchased"},
		{http.StatusTooManyRequests, "PrepaidBillOverdue"},
		{http.StatusTooManyRequests, "PostpaidBillOverdue"},
		{http.StatusTooManyRequests, "BudgetLimitExceeded"},
	} {
		t.Run(testCase.code, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, qwenDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(errorReply(testCase.status, testCase.code))
			result := runGenerateText(t, newQwenClient(t, provider.BaseURL(), ""), userRequest("Hello"))
			require.Equal(t, qwen.GenerateTextBranchProviderRejected, result.Branch)
			require.Equal(t, sdkgo.FailureQuotaExhausted, result.Failure.Kind)
		})
	}
	for _, code := range []string{"limit_requests", "insufficient_quota", "limit_burst_rate", "Throttling.ServiceOverloaded"} {
		t.Run(code, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, qwenDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(errorReply(http.StatusTooManyRequests, code))
			_, err := sdkgo.RunQuery(testsupport.NewDexContext("qwen-flow", "qwen-throttled-"+code),
				newQwenClient(t, provider.BaseURL(), "").GenerateText(), testConnection, userRequest("Hello"))
			var retryError *sdkgo.RetryError
			require.ErrorAs(t, err, &retryError, "per-minute throttling recovers, so it returns Retry")
			require.Equal(t, sdkgo.FailureRateLimit, retryError.Failure.Kind)
		})
	}
}

// TestGenerateTextCallsTheConfiguredDashScopeEndpoint proves the production URLs without a network call.
func TestGenerateTextCallsTheConfiguredDashScopeEndpoint(t *testing.T) {
	for _, testCase := range []struct{ endpoint, url string }{
		{"", "https://dashscope-intl.aliyuncs.com/compatible-mode/v1/chat/completions"},
		{"https://cn-hongkong.dashscope.aliyuncs.com/compatible-mode/v1/", "https://cn-hongkong.dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"},
	} {
		t.Run(testCase.url, func(t *testing.T) {
			reply := qwenDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Hello.", ServedModel: "qwen3.7-plus"})
			var requestedURLs []string
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requestedURLs = append(requestedURLs, request.URL.String())
				return &http.Response{
					StatusCode: http.StatusOK, Header: reply.Header.Clone(), Body: io.NopCloser(strings.NewReader(reply.Body)), Request: request,
				}, nil
			})
			client, err := qwen.New(qwen.Config{Endpoint: testCase.endpoint}, testCredentials(),
				qwen.WithHTTPClient(&http.Client{Transport: transport}))
			require.NoError(t, err)
			result := runGenerateText(t, client, userRequest("Hello"))
			require.Equal(t, qwen.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, "qwen3.7-plus", result.Value.RequestedModel, "a blank connection model uses the manifest default")
			require.Equal(t, []string{testCase.url}, requestedURLs)
		})
	}
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	for _, endpoint := range []string{
		"https://llm-abc123.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1",
		"https://dashscope-us.aliyuncs.com/compatible-mode/v1",
		"https://trial.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1",
		"http://dashscope-intl.aliyuncs.com/compatible-mode/v1",
		"https://dashscope-intl.aliyuncs.com/api/v1",
		"https://dashscope-intl.aliyuncs.com.example.com/compatible-mode/v1",
		"https://dashscope-intl.aliyuncs.com/compatible-mode/v1?region=sg",
		"https://attacker.example/compatible-mode/v1",
	} {
		_, err := qwen.New(qwen.Config{Endpoint: endpoint}, testCredentials())
		require.Error(t, err, endpoint)
		require.NotContains(t, err.Error(), endpoint, "errors never repeat configuration values")
	}
	for _, baseURL := range []string{"https://dashscope-intl.aliyuncs.com.example.com", "http://192.0.2.10:8080", "https://attacker.example", ""} {
		_, err := qwen.New(qwen.Config{}, testCredentials(), qwen.WithBaseURLForTest(baseURL))
		require.ErrorContains(t, err, "loopback", baseURL)
	}
	for _, baseURL := range []string{"http://127.0.0.1:8080", "http://localhost:9000", "http://[::1]:7000"} {
		_, err := qwen.New(qwen.Config{}, testCredentials(), qwen.WithBaseURLForTest(baseURL))
		require.NoError(t, err, baseURL)
	}
	_, err := qwen.New(qwen.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = qwen.New(qwen.Config{Model: "qwen3.7-plus-é"}, testCredentials())
	require.Error(t, err, "an invalid connection model fails at startup")
	_, err = qwen.New(qwen.Config{MaxResponseBytes: -1}, testCredentials())
	require.ErrorContains(t, err, "maxResponseBytes")
	_, err = qwen.New(qwen.Config{}, testCredentials(), nil)
	require.ErrorContains(t, err, "option is nil")
}

func newQwenQuery(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
	return newFakeProviderClient(t, connection).GenerateText()
}

// newFakeProviderClient builds a Client for an llmtest suite's fake provider and connection.
func newFakeProviderClient(t testing.TB, connection llmtest.FakeConnection) *qwen.Client {
	t.Helper()
	client, err := qwen.New(qwen.Config{Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[qwen.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		qwen.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newQwenClient(t *testing.T, baseURL string, model string) *qwen.Client {
	t.Helper()
	client, err := qwen.New(qwen.Config{Model: model}, testCredentials(), qwen.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

// generateAndReadRequestBody runs one generated exchange and returns the decoded request body.
func generateAndReadRequestBody(t *testing.T, request qwen.GenerateTextRequest) map[string]any {
	t.Helper()
	provider := llmtest.NewFakeProvider(t, qwenDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(qwenDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Done.", ServedModel: request.Model}))
	result := runGenerateText(t, newQwenClient(t, provider.BaseURL(), ""), request)
	require.Equal(t, qwen.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	var body map[string]any
	require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
	return body
}

func testCredentials() sdkgo.StaticCredentialProvider[qwen.Credentials] {
	return sdkgo.StaticCredentialProvider[qwen.Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
}

func runGenerateText(t *testing.T, client *qwen.Client, request qwen.GenerateTextRequest) qwen.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("qwen-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("qwen-flow", stepExecutionID), client.GenerateText(), testConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), testAPIKey), "a Result contains the API key")
	return result
}

func userRequest(text string) qwen.GenerateTextRequest {
	return qwen.GenerateTextRequest{Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: text}}}
}

// errorReply carries the documented code only in error.code, so rules must not depend on error.type.
func errorReply(status int, code string) llmtest.FakeReply {
	return llmtest.FakeReply{
		StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
		Body: fmt.Sprintf(`{"error":{"message":"Request failed.","param":null,"code":%q},"request_id":"req-qwen-1"}`, code),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
