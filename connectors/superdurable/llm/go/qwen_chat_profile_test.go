// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/openaichat/openaichattest"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
)

const qwenTestAPIKey = "sk-qwen-connector-test-key-0123456789"

var qwenTestConnection = sdkgo.ConnectionRef{Provider: "llm", Name: "qwen-test"}

// qwenDialect matches chatProfile: streamed replies, the 400 Arrearage billing error, and the 400 moderation error.
var qwenDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "qwen3.7-plus", AlternateModel: "qwen3.8-flash", IsStreaming: true,
	QuotaExhaustedStatusCode: http.StatusBadRequest, QuotaExhaustedErrorToken: "Arrearage",
	ContentPolicyErrorToken: "data_inspection_failed",
})

func TestQwenGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureTwo := 2.0
	textgentest.RunTextGenerationExchangeSuite(t, &textgentest.TextGenerationExchangeSuite{
		Dialect:  qwenDialect,
		NewQuery: newQwenQuery,
		LocallyRejectedRequests: []textgentest.NamedTextGenerationRequest{
			{Name: "temperature 2, outside the half-open [0, 2) range", Request: textgen.TextGenerationRequest{Temperature: &temperatureTwo}},
			{Name: "reasoning effort on qwen3.7-plus, which documents none", Request: textgen.TextGenerationRequest{
				ReasoningEffort: textgen.ReasoningEffortHigh,
			}},
			{Name: "high reasoning effort, which Qwen3.8 silently maps to xhigh", Request: textgen.TextGenerationRequest{
				Model: "qwen3.8-max", ReasoningEffort: textgen.ReasoningEffortHigh,
			}},
			{Name: "max reasoning effort on qwen3.8-flash", Request: textgen.TextGenerationRequest{
				Model: "qwen3.8-flash", ReasoningEffort: textgen.ReasoningEffortMax,
			}},
			{Name: "reasoning effort none on the thinking-only qwen3.8-2.4t-a95b", Request: textgen.TextGenerationRequest{
				Model: "qwen3.8-2.4t-a95b", ReasoningEffort: textgen.ReasoningEffortNone,
			}},
		},
	})
}

// TestQwenGenerateTextSendsTheDocumentedChatCompletionsRequest pins the body fields Model Studio documents for Chat Completions.
func TestQwenGenerateTextSendsTheDocumentedChatCompletionsRequest(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, qwenDialect.CredentialHeader, qwenTestAPIKey)
	provider.EnqueueReplies(qwenDialect.GeneratedReply(textgentest.GeneratedReply{
		Text: `{"answer":"TCP is reliable."}`, ServedModel: "qwen3.7-plus", ResponseID: "chatcmpl-qwen-1",
		Usage: textgen.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44},
	}))
	temperature := 0.4
	result := runQwenGenerateText(t, newQwenClient(t, provider.BaseURL(), ""), llm.GenerateTextRequest{
		Instructions: "Answer in one sentence.",
		Messages:     []textgen.Message{{Role: textgen.MessageRoleUser, Text: "How does TCP differ from UDP?"}},
		StructuredOutput: &textgen.StructuredOutput{Name: "answer", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
			"required": []any{"answer"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature,
	})

	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
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

// TestQwenGenerateTextSelectsTheDocumentedTokenLimitField sends max_completion_tokens only where Model Studio documents it.
func TestQwenGenerateTextSelectsTheDocumentedTokenLimitField(t *testing.T) {
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
			body := generateQwenTextAndReadRequestBody(t, llm.GenerateTextRequest{
				Model: testCase.model, Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: "Hello"}}, MaxOutputTokens: 512,
			})
			require.Equal(t, float64(512), body[testCase.field])
			require.Len(t, body, 5, "model, messages, one token-limit field, stream, and stream_options")
		})
	}
}

// TestQwenGenerateTextMapsQwen38ReasoningEfforts sends only the efforts Qwen3.8 documents as native values.
func TestQwenGenerateTextMapsQwen38ReasoningEfforts(t *testing.T) {
	for _, testCase := range []struct {
		model  string
		effort textgen.ReasoningEffort
		wire   string
	}{
		{"qwen3.8-flash", textgen.ReasoningEffortNone, "none"},
		{"qwen3.8-flash", textgen.ReasoningEffortLow, "low"},
		{"qwen3.8-max", textgen.ReasoningEffortMedium, "medium"},
		{"qwen3.8-max-0902", textgen.ReasoningEffortExtraHigh, "xhigh"},
		{"qwen3.8-27b", textgen.ReasoningEffortNone, "none"},
		{"qwen3.8-2.4t-a95b", textgen.ReasoningEffortLow, "low"},
	} {
		t.Run(testCase.model+"/"+string(testCase.effort), func(t *testing.T) {
			body := generateQwenTextAndReadRequestBody(t, llm.GenerateTextRequest{
				Model: testCase.model, Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: "Plan the migration."}},
				ReasoningEffort: testCase.effort,
			})
			require.Equal(t, testCase.wire, body["reasoning_effort"])
		})
	}
}

// TestQwenContentModerationSelectsBlocked covers the native token and an error object inside a stream.
func TestQwenContentModerationSelectsBlocked(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		reply textgentest.FakeReply
	}{
		{"native DataInspectionFailed code", textgentest.FakeReply{
			StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": {"application/json"}},
			Body: `{"error":{"message":"Input data may contain inappropriate content.","type":"DataInspectionFailed","param":null,"code":"DataInspectionFailed"}}`,
		}},
		{"error object inside the stream", textgentest.FakeReply{
			Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: `data: {"id":"chatcmpl-qwen-2","object":"chat.completion.chunk","model":"qwen3.7-plus",` +
				`"choices":[{"index":0,"delta":{"role":"assistant","content":"Partial"},"finish_reason":null}]}` + "\n\n" +
				`data: {"error":{"message":"Output data may contain inappropriate content.","type":"data_inspection_failed","param":null,"code":"data_inspection_failed"}}` + "\n\n",
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, qwenDialect.CredentialHeader, qwenTestAPIKey)
			provider.EnqueueReplies(testCase.reply)
			result := runQwenGenerateText(t, newQwenClient(t, provider.BaseURL(), ""), qwenUserRequest("Hello"))
			require.Equal(t, llm.GenerateTextBranchBlocked, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, textgen.FinishReasonContentPolicy, result.Value.FinishReason)
			require.Empty(t, result.Value.Text)
			require.NotContains(t, result.Failure.Message, "inappropriate", "a Failure never carries provider message text")
		})
	}
}

// TestQwenAccountAndBillingErrorsAreNotRetried keeps billing states on providerRejected and throttling on Retry.
func TestQwenAccountAndBillingErrorsAreNotRetried(t *testing.T) {
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
			provider := textgentest.NewFakeProvider(t, qwenDialect.CredentialHeader, qwenTestAPIKey)
			provider.EnqueueReplies(qwenErrorReply(testCase.status, testCase.code))
			result := runQwenGenerateText(t, newQwenClient(t, provider.BaseURL(), ""), qwenUserRequest("Hello"))
			require.Equal(t, llm.GenerateTextBranchProviderRejected, result.Branch)
			require.Equal(t, sdkgo.FailureQuotaExhausted, result.Failure.Kind)
		})
	}
	for _, code := range []string{"limit_requests", "insufficient_quota", "limit_burst_rate", "Throttling.ServiceOverloaded"} {
		t.Run(code, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, qwenDialect.CredentialHeader, qwenTestAPIKey)
			provider.EnqueueReplies(qwenErrorReply(http.StatusTooManyRequests, code))
			_, err := sdkgo.RunQuery(testsupport.NewDexContext("qwen-flow", "qwen-throttled-"+code),
				newQwenClient(t, provider.BaseURL(), "").GenerateText(), qwenTestConnection, qwenUserRequest("Hello"))
			var retryError *sdkgo.RetryError
			require.ErrorAs(t, err, &retryError, "per-minute throttling recovers, so it returns Retry")
			require.Equal(t, sdkgo.FailureRateLimit, retryError.Failure.Kind)
		})
	}
}

func newQwenQuery(t testing.TB, connection textgentest.FakeConnection) *textgen.TextGenerationQuery {
	return newQwenFakeProviderClient(t, connection).GenerateText()
}

// newQwenFakeProviderClient builds a Client for a textgentest suite's fake provider and connection.
func newQwenFakeProviderClient(t testing.TB, connection textgentest.FakeConnection) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderQwen, Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[llm.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		llm.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newQwenClient(t *testing.T, baseURL string, model string) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderQwen, Model: model}, qwenTestCredentials(), llm.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

// generateQwenTextAndReadRequestBody runs one generated exchange and returns the decoded request body.
func generateQwenTextAndReadRequestBody(t *testing.T, request llm.GenerateTextRequest) map[string]any {
	t.Helper()
	provider := textgentest.NewFakeProvider(t, qwenDialect.CredentialHeader, qwenTestAPIKey)
	provider.EnqueueReplies(qwenDialect.GeneratedReply(textgentest.GeneratedReply{Text: "Done.", ServedModel: request.Model}))
	result := runQwenGenerateText(t, newQwenClient(t, provider.BaseURL(), ""), request)
	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	var body map[string]any
	require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
	return body
}

func qwenTestCredentials() sdkgo.StaticCredentialProvider[llm.Credentials] {
	return sdkgo.StaticCredentialProvider[llm.Credentials]{qwenTestConnection: {APIKey: sdkgo.NewSecretString(qwenTestAPIKey)}}
}

func runQwenGenerateText(t *testing.T, client *llm.Client, request llm.GenerateTextRequest) llm.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("qwen-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("qwen-flow", stepExecutionID), client.GenerateText(), qwenTestConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), qwenTestAPIKey), "a Result contains the API key")
	return result
}

func qwenUserRequest(text string) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: text}}}
}

// qwenErrorReply carries the documented code only in error.code, so rules must not depend on error.type.
func qwenErrorReply(status int, code string) textgentest.FakeReply {
	return textgentest.FakeReply{
		StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
		Body: fmt.Sprintf(`{"error":{"message":"Request failed.","param":null,"code":%q},"request_id":"req-qwen-1"}`, code),
	}
}
