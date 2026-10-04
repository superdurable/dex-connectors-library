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
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/messagestest"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
)

const (
	claudeTestAPIKey      = "claude-connector-test-key-canary"
	claudeTestWorkspaceID = "wrkspc_01JwQvzr7rXLA5AGx3HKfFUJ"
)

var claudeTestConnection = sdkgo.ConnectionRef{Provider: "llm", Name: "claude-test"}

// claudeDialect streams Messages replies, reports the spend-cap 429 as quota, and uses Claude's error envelope.
var claudeDialect = messagestest.NewProviderDialect("claude-sonnet-5", "claude-haiku-4-5")

func TestClaudeGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureBelowDefault, temperatureAboveOne := 0.5, 1.5
	textgentest.RunTextGenerationExchangeSuite(t, &textgentest.TextGenerationExchangeSuite{
		Dialect:  claudeDialect,
		NewQuery: newClaudeQuery,
		LocallyRejectedRequests: []textgentest.NamedTextGenerationRequest{
			{Name: "temperature other than 1.0 on a model after Claude Opus 4.6", Request: textgen.TextGenerationRequest{
				Temperature: &temperatureBelowDefault,
			}},
			{Name: "temperature above 1 on Claude Haiku 4.5", Request: textgen.TextGenerationRequest{
				Model: "claude-haiku-4-5", Temperature: &temperatureAboveOne,
			}},
			{Name: "reasoning effort none, which Claude does not offer", Request: textgen.TextGenerationRequest{
				ReasoningEffort: textgen.ReasoningEffortNone,
			}},
			{Name: "reasoning effort minimal, which Claude does not offer", Request: textgen.TextGenerationRequest{
				ReasoningEffort: textgen.ReasoningEffortMinimal,
			}},
			{Name: "xhigh effort on Claude Sonnet 4.6", Request: textgen.TextGenerationRequest{
				Model: "claude-sonnet-4-6", ReasoningEffort: textgen.ReasoningEffortExtraHigh,
			}},
			{Name: "max effort on Claude Opus 4.5", Request: textgen.TextGenerationRequest{
				Model: "claude-opus-4-5-20251101", ReasoningEffort: textgen.ReasoningEffortMax,
			}},
			{Name: "effort on Claude Haiku 4.5, which has none", Request: textgen.TextGenerationRequest{
				Model: "claude-haiku-4-5-20251001", ReasoningEffort: textgen.ReasoningEffortLow,
			}},
		},
	})
}

// TestClaudeGenerateTextSendsTheDocumentedMessagesRequest pins the headers and body fields Claude documents for Messages.
func TestClaudeGenerateTextSendsTheDocumentedMessagesRequest(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, claudeDialect.CredentialHeader, claudeTestAPIKey)
	reply := messagestest.StreamReply(textgentest.GeneratedReply{
		Text: `{"answer":"TCP is reliable.","confidence":9}`, ServedModel: "claude-sonnet-5", ResponseID: "msg_claude_1",
		Usage: textgen.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12},
	}, "end_turn")
	reply.Header.Set("request-id", "req_011CSHoEeqs5C35K2UUqR7Fy")
	reply.Header.Set("anthropic-ratelimit-requests-remaining", "49")
	reply.Header.Set("anthropic-ratelimit-output-tokens-remaining", "79000")
	provider.EnqueueReplies(reply)
	temperature := 1.0
	result := runClaudeGenerateText(t, newClaudeClient(t, provider.BaseURL(), llm.Config{Provider: llm.ProviderAnthropic, AnthropicWorkspaceID: " " + claudeTestWorkspaceID + " "}), llm.GenerateTextRequest{
		Instructions: "Answer in one sentence.",
		Messages: []textgen.Message{
			{Role: textgen.MessageRoleUser, Text: "How does TCP differ from UDP?"},
			{Role: textgen.MessageRoleAssistant, Text: "Do you want a short answer?"},
			{Role: textgen.MessageRoleUser, Text: "Yes."},
		},
		StructuredOutput: &textgen.StructuredOutput{Name: "answer", Description: "A short answer.", Schema: map[string]any{
			"type": "object", "properties": map[string]any{
				"answer":     map[string]any{"type": "string", "maxLength": 200},
				"confidence": map[string]any{"type": "integer", "minimum": 0, "maximum": 10},
			},
			"required": []any{"answer", "confidence"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature, ReasoningEffort: textgen.ReasoningEffortExtraHigh,
	})

	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, `{"answer":"TCP is reliable.","confidence":9}`, result.Value.Text)
	require.Equal(t, "claude-sonnet-5", result.Value.RequestedModel)
	require.Equal(t, "end_turn", result.Value.ProviderFinishReason)
	require.Equal(t, textgen.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44}, result.Value.Usage)
	require.Equal(t, "anthropic", result.Receipt.Provider)
	require.Equal(t, "msg_claude_1", result.Receipt.ProviderObjectID)
	require.Equal(t, "req_011CSHoEeqs5C35K2UUqR7Fy", result.Receipt.ProviderRequestID)
	require.Equal(t, map[string]string{
		"anthropic-ratelimit-requests-remaining": "49", "anthropic-ratelimit-output-tokens-remaining": "79000",
	}, result.Receipt.Metadata)
	requests := provider.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].Method)
	require.Equal(t, "/v1/messages", requests[0].Path)
	require.Equal(t, "2023-06-01", requests[0].Header.Get("anthropic-version"))
	require.Equal(t, claudeTestWorkspaceID, requests[0].Header.Get("anthropic-workspace-id"))
	require.Equal(t, "text/event-stream", requests[0].Header.Get("Accept"))
	require.Equal(t, "application/json", requests[0].Header.Get("Content-Type"))
	require.Empty(t, requests[0].Header.Get("x-api-key"), "the key travels only as an Authorization bearer token")
	require.True(t, requests[0].HasCredentialInSlot, "the key travels as an Authorization bearer token")
	require.False(t, requests[0].HasCredentialOutsideSlot, "the key never reaches the body, path, or another header")
	var body map[string]any
	require.NoError(t, json.Unmarshal(requests[0].Body, &body))
	require.Equal(t, map[string]any{
		"model":      "claude-sonnet-5",
		"max_tokens": float64(2048),
		"system":     "Answer in one sentence.",
		"messages": []any{
			map[string]any{"role": "user", "content": "How does TCP differ from UDP?"},
			map[string]any{"role": "assistant", "content": "Do you want a short answer?"},
			map[string]any{"role": "user", "content": "Yes."},
		},
		"temperature": 1.0,
		"output_config": map[string]any{"effort": "xhigh", "format": map[string]any{"type": "json_schema", "schema": map[string]any{
			"type": "object", "description": "A short answer.", "properties": map[string]any{
				"answer":     map[string]any{"type": "string", "description": "(maxLength: 200)"},
				"confidence": map[string]any{"type": "integer", "description": "(maximum: 10, minimum: 0)"},
			},
			"required": []any{"answer", "confidence"}, "additionalProperties": false,
		}}},
		"stream": true,
	}, body)
}

func TestClaudeGenerateTextSendsTheMinimalMessagesRequest(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, claudeDialect.CredentialHeader, claudeTestAPIKey)
	provider.EnqueueReplies(claudeDialect.GeneratedReply(textgentest.GeneratedReply{Text: "Hello.", ServedModel: "claude-sonnet-5"}))
	result := runClaudeGenerateText(t, newClaudeClient(t, provider.BaseURL(), llm.Config{Provider: llm.ProviderAnthropic}), claudeUserRequest("Hello"))
	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "Hello.", result.Value.Text, "thinking never reaches the text")
	requests := provider.Requests()
	require.Empty(t, requests[0].Header.Values("anthropic-workspace-id"), "a blank anthropicWorkspaceId sends no header")
	require.JSONEq(t, `{"model":"claude-sonnet-5","max_tokens":16000,"messages":[{"role":"user","content":"Hello"}],"stream":true}`,
		string(requests[0].Body), "Claude requires max_tokens, so the connector default applies")
}

// TestClaudeDefaultMaxOutputTokensFitsOneExchange keeps a full-length default generation, the 16000
// max_tokens that TestClaudeGenerateTextSendsTheMinimalMessagesRequest pins, inside the 870-second request
// timeout at the 128,000 output tokens per hour Anthropic's SDKs estimate, so it ends as truncated instead of
// timing out and retrying.
func TestClaudeDefaultMaxOutputTokensFitsOneExchange(t *testing.T) {
	const requestTimeout, defaultMaxOutputTokens, sdkEstimatedTokensPerHour = 870 * time.Second, 16000, 128000
	expected := time.Duration(defaultMaxOutputTokens) * time.Hour / sdkEstimatedTokensPerHour
	require.Less(t, expected, requestTimeout*3/5, "the default leaves headroom for queueing and slower models")
	require.Equal(t, 1200*time.Second, llm.GenerateTextDefinition.StepDefaults.ExecuteMethodTimeout)

	_, err := llm.New(llm.Config{Provider: llm.ProviderAnthropic}, claudeTestCredentials(), llm.WithHTTPClient(&http.Client{Timeout: 1200 * time.Second}))
	require.ErrorContains(t, err, "Execute timeout", "an exchange cannot outlast the Execute timeout")
	_, err = llm.New(llm.Config{Provider: llm.ProviderAnthropic}, claudeTestCredentials(), llm.WithHTTPClient(&http.Client{Timeout: 600 * time.Second}))
	require.NoError(t, err, "a shorter exchange is accepted")
}

func TestClaudeGenerateTextAppliesThePerModelRules(t *testing.T) {
	temperature := 0.3
	for _, testCase := range []struct {
		name, model        string
		request            llm.GenerateTextRequest
		expectedBodyFields map[string]any
	}{
		{"default output limit", "claude-opus-5-5", llm.GenerateTextRequest{}, map[string]any{"max_tokens": float64(16000)}},
		{"max effort on Claude Opus 5.5", "claude-opus-5-5", llm.GenerateTextRequest{ReasoningEffort: textgen.ReasoningEffortMax},
			map[string]any{"output_config": map[string]any{"effort": "max"}}},
		{"temperature on Claude Sonnet 4.6", "claude-sonnet-4-6", llm.GenerateTextRequest{Temperature: &temperature},
			map[string]any{"temperature": 0.3}},
		{"max effort on Claude Sonnet 4.6", "claude-sonnet-4-6", llm.GenerateTextRequest{ReasoningEffort: textgen.ReasoningEffortMax},
			map[string]any{"output_config": map[string]any{"effort": "max"}}},
		{"temperature on dated Claude Haiku 4.5", "claude-haiku-4-5-20251001", llm.GenerateTextRequest{Temperature: &temperature},
			map[string]any{"temperature": 0.3}},
		{"high effort on Claude Opus 4.5", "claude-opus-4-5", llm.GenerateTextRequest{ReasoningEffort: textgen.ReasoningEffortHigh},
			map[string]any{"output_config": map[string]any{"effort": "high"}}},
		{"xhigh effort on a model this release does not name", "claude-sonnet-5-1",
			llm.GenerateTextRequest{ReasoningEffort: textgen.ReasoningEffortExtraHigh},
			map[string]any{"output_config": map[string]any{"effort": "xhigh"}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, claudeDialect.CredentialHeader, claudeTestAPIKey)
			provider.EnqueueReplies(claudeDialect.GeneratedReply(textgentest.GeneratedReply{Text: "Done.", ServedModel: testCase.model}))
			request := testCase.request
			request.Model, request.Messages = testCase.model, claudeUserRequest("Plan the migration.").Messages
			result := runClaudeGenerateText(t, newClaudeClient(t, provider.BaseURL(), llm.Config{Provider: llm.ProviderAnthropic}), request)
			require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			var body map[string]any
			require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
			for field, value := range testCase.expectedBodyFields {
				require.Equal(t, value, body[field], field)
			}
		})
	}
}

// TestClaudeStreamOutcomesFollowClaudeStopReasons covers the stop reasons and stream shapes the shared suite does not.
func TestClaudeStreamOutcomesFollowClaudeStopReasons(t *testing.T) {
	message := textgentest.GeneratedReply{ServedModel: "claude-sonnet-5", ResponseID: "msg_claude_2", Usage: textgen.Usage{InputTokens: 9, OutputTokens: 3}}
	textBlockStart := messagestest.Event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
	stop := func(stopReason string) string {
		return messagestest.Event("message_delta", map[string]any{"delta": map[string]any{"stop_reason": stopReason}, "usage": map[string]any{"output_tokens": 3}}) +
			messagestest.Event("message_stop", map[string]any{})
	}
	for _, testCase := range []struct {
		name   string
		stream string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		text   string
	}{
		{"context window exhausted keeps the partial text", messagestest.MessageStartEvent(message) + textBlockStart +
			messagestest.TextDeltaEvent(0, "Partial") + stop("model_context_window_exceeded"),
			llm.GenerateTextBranchTruncated, sdkgo.FailureResponseTooLarge, "Partial"},
		{"a refusal after streamed text returns no text", messagestest.MessageStartEvent(message) + textBlockStart +
			messagestest.TextDeltaEvent(0, "I can") + stop("refusal"),
			llm.GenerateTextBranchBlocked, sdkgo.FailureProviderRejection, ""},
		{"tool_use without tools is unusable", messagestest.MessageStartEvent(message) + textBlockStart +
			messagestest.TextDeltaEvent(0, "Calling") + stop("tool_use"),
			llm.GenerateTextBranchInvalidResponse, sdkgo.FailureProtocol, ""},
		{"a text delta for a thinking block is malformed", messagestest.MessageStartEvent(message) +
			messagestest.Event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "thinking", "thinking": ""}}) +
			messagestest.TextDeltaEvent(0, "Leaked") + stop("end_turn"),
			llm.GenerateTextBranchInvalidResponse, sdkgo.FailureProtocol, ""},
		{"content before message_start is malformed", textBlockStart + messagestest.TextDeltaEvent(0, "Early") + stop("end_turn"),
			llm.GenerateTextBranchInvalidResponse, sdkgo.FailureProtocol, ""},
		{"unknown event types are ignored", messagestest.MessageStartEvent(message) +
			messagestest.Event("llmtest_future_event", map[string]any{"detail": "ignored"}) + textBlockStart +
			messagestest.TextDeltaEvent(0, "Done.") + stop("end_turn"),
			llm.GenerateTextBranchGenerated, "", "Done."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, claudeDialect.CredentialHeader, claudeTestAPIKey)
			provider.EnqueueReplies(messagestest.EventStreamReply(testCase.stream))
			result := runClaudeGenerateText(t, newClaudeClient(t, provider.BaseURL(), llm.Config{Provider: llm.ProviderAnthropic}), claudeUserRequest("Hello"))
			require.Equal(t, testCase.branch, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, testCase.text, result.Value.Text)
			if testCase.kind != "" {
				require.Equal(t, testCase.kind, result.Failure.Kind)
			}
		})
	}
}

// TestClaudeMidStreamErrorEventsAreClassified follows Claude's error types for an error event after a 200 status.
func TestClaudeMidStreamErrorEventsAreClassified(t *testing.T) {
	message := textgentest.GeneratedReply{ServedModel: "claude-sonnet-5", ResponseID: "msg_claude_3"}
	partialStream := messagestest.MessageStartEvent(message) +
		messagestest.Event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}}) +
		messagestest.TextDeltaEvent(0, "Partial")
	for _, errorType := range []string{"overloaded_error", "api_error", "timeout_error"} {
		t.Run(errorType, func(t *testing.T) {
			requireClaudeRetry(t, messagestest.EventStreamReply(partialStream+messagestest.ErrorEvent(errorType, "Overloaded")), sdkgo.FailureAvailability)
		})
	}
	t.Run("rate_limit_error", func(t *testing.T) {
		requireClaudeRetry(t, messagestest.EventStreamReply(partialStream+messagestest.ErrorEvent("rate_limit_error", "Slow down")), sdkgo.FailureRateLimit)
	})
	t.Run("billing_error is quota", func(t *testing.T) {
		provider := textgentest.NewFakeProvider(t, claudeDialect.CredentialHeader, claudeTestAPIKey)
		provider.EnqueueReplies(messagestest.EventStreamReply(partialStream + messagestest.ErrorEvent("billing_error", "Pay message-canary")))
		result := runClaudeGenerateText(t, newClaudeClient(t, provider.BaseURL(), llm.Config{Provider: llm.ProviderAnthropic}), claudeUserRequest("Hello"))
		require.Equal(t, llm.GenerateTextBranchProviderRejected, result.Branch, "failure: %+v", result.Failure)
		require.Equal(t, sdkgo.FailureQuotaExhausted, result.Failure.Kind)
		require.NotContains(t, result.Failure.Message, "message-canary", "a Failure never carries provider message text")
		require.Empty(t, result.Value.Text)
	})
	t.Run("invalid_request_error is unusable", func(t *testing.T) {
		provider := textgentest.NewFakeProvider(t, claudeDialect.CredentialHeader, claudeTestAPIKey)
		provider.EnqueueReplies(messagestest.EventStreamReply(partialStream + messagestest.ErrorEvent("invalid_request_error", "Bad")))
		result := runClaudeGenerateText(t, newClaudeClient(t, provider.BaseURL(), llm.Config{Provider: llm.ProviderAnthropic}), claudeUserRequest("Hello"))
		require.Equal(t, llm.GenerateTextBranchInvalidResponse, result.Branch)
		require.Contains(t, result.Failure.Message, "invalid_request_error")
		require.Empty(t, result.Value.Text)
	})
}

// TestClaudeUnstreamedJSONBodiesAreClassified covers a gateway that answers the streaming request with a complete
// 2xx JSON body, which is classified instead of being retried as an interrupted stream.
func TestClaudeUnstreamedJSONBodiesAreClassified(t *testing.T) {
	t.Run("a 200 overloaded_error envelope returns Retry", func(t *testing.T) {
		requireClaudeRetry(t, messagestest.ErrorBodyReply(http.StatusOK, "overloaded_error", "Overloaded", ""), sdkgo.FailureAvailability)
	})
	t.Run("a 200 spend-cap envelope is quota", func(t *testing.T) {
		provider := textgentest.NewFakeProvider(t, claudeDialect.CredentialHeader, claudeTestAPIKey)
		provider.EnqueueReplies(messagestest.ErrorBodyReply(http.StatusOK, "rate_limit_error", "Cap message-canary", messagestest.SpendLimitErrorCode))
		result := runClaudeGenerateText(t, newClaudeClient(t, provider.BaseURL(), llm.Config{Provider: llm.ProviderAnthropic}), claudeUserRequest("Hello"))
		require.Equal(t, llm.GenerateTextBranchProviderRejected, result.Branch, "failure: %+v", result.Failure)
		require.Equal(t, sdkgo.FailureQuotaExhausted, result.Failure.Kind)
		require.NotContains(t, result.Failure.Message, "message-canary", "a Failure never carries provider message text")
	})
	for _, testCase := range []struct{ name, body string }{
		{"a 200 body of another type is unusable", `{"type":"completion","completion":"message-canary"}`},
		{"a 200 body that is not JSON is unusable", `message-canary`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, claudeDialect.CredentialHeader, claudeTestAPIKey)
			provider.EnqueueReplies(textgentest.FakeReply{Header: http.Header{"Content-Type": {"application/json"}}, Body: testCase.body})
			result := runClaudeGenerateText(t, newClaudeClient(t, provider.BaseURL(), llm.Config{Provider: llm.ProviderAnthropic}), claudeUserRequest("Hello"))
			require.Equal(t, llm.GenerateTextBranchInvalidResponse, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "message-canary", "a Failure never carries provider body text")
			require.Empty(t, result.Value.Text)
		})
	}
}

// TestClaudeErrorStatusesFollowClaudeErrorTypes covers the documented statuses the shared suite does not.
func TestClaudeErrorStatusesFollowClaudeErrorTypes(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		reply textgentest.FakeReply
		kind  sdkgo.FailureKind
	}{
		{"402 billing_error", messagestest.ErrorReply(http.StatusPaymentRequired, "Check your payment message-canary."), sdkgo.FailureQuotaExhausted},
		{"404 not_found_error for an unknown model", messagestest.ErrorReply(http.StatusNotFound, "model: message-canary"), sdkgo.FailureNotFound},
		{"413 request_too_large", messagestest.ErrorReply(http.StatusRequestEntityTooLarge, "Too large message-canary."), sdkgo.FailureProviderRejection},
		{"501 api_error stays a rejection", messagestest.ErrorBodyReply(http.StatusNotImplemented, "api_error", "Not implemented message-canary.", ""),
			sdkgo.FailureProviderRejection},
		{"400 spend limit you set", messagestest.ErrorReply(http.StatusBadRequest, "You have reached your specified API usage limits message-canary."),
			sdkgo.FailureProviderRejection},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, claudeDialect.CredentialHeader, claudeTestAPIKey)
			provider.EnqueueReplies(testCase.reply)
			result := runClaudeGenerateText(t, newClaudeClient(t, provider.BaseURL(), llm.Config{Provider: llm.ProviderAnthropic}), claudeUserRequest("Hello"))
			require.Equal(t, llm.GenerateTextBranchProviderRejected, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "message-canary", "a Failure never carries provider message text")
		})
	}
	t.Run("529 overloaded_error", func(t *testing.T) {
		requireClaudeRetry(t, messagestest.ErrorReply(529, "Overloaded"), sdkgo.FailureAvailability)
	})
}

func TestClaudeNewRejectsUnsafeConfiguration(t *testing.T) {
	for _, baseURL := range []string{"https://api.anthropic.com.example.com", "http://192.0.2.10:8080", "https://attacker.example"} {
		_, err := llm.New(llm.Config{Provider: llm.ProviderAnthropic}, claudeTestCredentials(), llm.WithBaseURLForTest(baseURL))
		require.ErrorContains(t, err, "loopback", baseURL)
	}
	for _, baseURL := range []string{"http://127.0.0.1:8080", "http://localhost:9000", "http://[::1]:7000"} {
		_, err := llm.New(llm.Config{Provider: llm.ProviderAnthropic}, claudeTestCredentials(), llm.WithBaseURLForTest(baseURL))
		require.NoError(t, err, baseURL)
	}
	_, err := llm.New(llm.Config{Provider: llm.ProviderAnthropic}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = llm.New(llm.Config{Provider: llm.ProviderAnthropic, Model: "claude-é"}, claudeTestCredentials())
	require.Error(t, err, "an invalid connection model fails at startup")
	for _, workspaceID := range []string{"workspace-1", "wrkspc_bad!", "wrkspc_01 space", "wrkspc_01\r\nX-Injected: 1"} {
		_, err = llm.New(llm.Config{Provider: llm.ProviderAnthropic, AnthropicWorkspaceID: workspaceID}, claudeTestCredentials())
		require.ErrorContains(t, err, "anthropicWorkspaceId", "%q", workspaceID)
		require.NotContains(t, err.Error(), workspaceID, "the error never repeats the value")
	}
	_, err = llm.New(llm.Config{Provider: llm.ProviderAnthropic, MaxResponseBytes: -1}, claudeTestCredentials())
	require.ErrorContains(t, err, "maxResponseBytes")
	_, err = llm.New(llm.Config{Provider: llm.ProviderAnthropic}, claudeTestCredentials(), nil)
	require.ErrorContains(t, err, "option is nil")
}

func newClaudeQuery(t testing.TB, connection textgentest.FakeConnection) *textgen.TextGenerationQuery {
	return newClaudeFakeProviderClient(t, connection).GenerateText()
}

// newClaudeFakeProviderClient builds a Client for a textgentest suite's fake provider and connection.
func newClaudeFakeProviderClient(t testing.TB, connection textgentest.FakeConnection) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderAnthropic, Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[llm.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		llm.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newClaudeClient(t *testing.T, baseURL string, config llm.Config) *llm.Client {
	t.Helper()
	client, err := llm.New(config, claudeTestCredentials(), llm.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func claudeTestCredentials() sdkgo.StaticCredentialProvider[llm.Credentials] {
	return sdkgo.StaticCredentialProvider[llm.Credentials]{claudeTestConnection: {APIKey: sdkgo.NewSecretString(claudeTestAPIKey)}}
}

func runClaudeGenerateText(t *testing.T, client *llm.Client, request llm.GenerateTextRequest) llm.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("claude-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("claude-flow", stepExecutionID), client.GenerateText(), claudeTestConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), claudeTestAPIKey), "a Result contains the API key")
	return result
}

// requireClaudeRetry runs one request against reply and requires Retry with kind and no provider message text.
func requireClaudeRetry(t *testing.T, reply textgentest.FakeReply, kind sdkgo.FailureKind) {
	t.Helper()
	provider := textgentest.NewFakeProvider(t, claudeDialect.CredentialHeader, claudeTestAPIKey)
	provider.EnqueueReplies(reply)
	_, err := sdkgo.RunQuery(testsupport.NewDexContext("claude-flow", fmt.Sprintf("claude-retry-%d", time.Now().UnixNano())),
		newClaudeClient(t, provider.BaseURL(), llm.Config{Provider: llm.ProviderAnthropic}).GenerateText(), claudeTestConnection, claudeUserRequest("Hello"))
	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError)
	require.Equal(t, kind, retryError.Failure.Kind, "failure message: %s", retryError.Failure.Message)
	require.NotContains(t, retryError.Failure.Message, "Overloaded", "a Failure never carries provider message text")
	require.NotContains(t, err.Error(), claudeTestAPIKey)
}

func claudeUserRequest(text string) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: text}}}
}
