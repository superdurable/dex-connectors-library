// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package claude_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	claude "github.com/superdurable/dex-connectors-library/connectors/anthropic"
	"github.com/superdurable/dex-connectors-library/connectors/anthropic/internal/messagestest"
	"github.com/superdurable/dex-connectors-library/connectors/anthropic/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
)

const (
	testAPIKey      = "claude-connector-test-key-canary"
	testWorkspaceID = "wrkspc_01JwQvzr7rXLA5AGx3HKfFUJ"
)

var testConnection = sdkgo.ConnectionRef{Provider: "anthropic", Name: "claude-test"}

// claudeDialect streams Messages replies, reports the spend-cap 429 as quota, and uses Claude's error envelope.
var claudeDialect = messagestest.NewProviderDialect("claude-sonnet-5", "claude-haiku-4-5")

func TestGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureBelowDefault, temperatureAboveOne := 0.5, 1.5
	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
		Dialect:  claudeDialect,
		NewQuery: newClaudeQuery,
		LocallyRejectedRequests: []llmtest.NamedTextGenerationRequest{
			{Name: "temperature other than 1.0 on a model after Claude Opus 4.6", Request: llm.TextGenerationRequest{
				Temperature: &temperatureBelowDefault,
			}},
			{Name: "temperature above 1 on Claude Haiku 4.5", Request: llm.TextGenerationRequest{
				Model: "claude-haiku-4-5", Temperature: &temperatureAboveOne,
			}},
			{Name: "reasoning effort none, which Claude does not offer", Request: llm.TextGenerationRequest{
				ReasoningEffort: llm.ReasoningEffortNone,
			}},
			{Name: "reasoning effort minimal, which Claude does not offer", Request: llm.TextGenerationRequest{
				ReasoningEffort: llm.ReasoningEffortMinimal,
			}},
			{Name: "xhigh effort on Claude Sonnet 4.6", Request: llm.TextGenerationRequest{
				Model: "claude-sonnet-4-6", ReasoningEffort: llm.ReasoningEffortExtraHigh,
			}},
			{Name: "max effort on Claude Opus 4.5", Request: llm.TextGenerationRequest{
				Model: "claude-opus-4-5-20251101", ReasoningEffort: llm.ReasoningEffortMax,
			}},
			{Name: "effort on Claude Haiku 4.5, which has none", Request: llm.TextGenerationRequest{
				Model: "claude-haiku-4-5-20251001", ReasoningEffort: llm.ReasoningEffortLow,
			}},
		},
	})
}

// TestGenerateTextSendsTheDocumentedMessagesRequest pins the headers and body fields Claude documents for Messages.
func TestGenerateTextSendsTheDocumentedMessagesRequest(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, claudeDialect.CredentialHeader, testAPIKey)
	reply := messagestest.StreamReply(llmtest.GeneratedReply{
		Text: `{"answer":"TCP is reliable.","confidence":9}`, ServedModel: "claude-sonnet-5", ResponseID: "msg_claude_1",
		Usage: llm.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12},
	}, "end_turn")
	reply.Header.Set("request-id", "req_011CSHoEeqs5C35K2UUqR7Fy")
	reply.Header.Set("anthropic-ratelimit-requests-remaining", "49")
	reply.Header.Set("anthropic-ratelimit-output-tokens-remaining", "79000")
	provider.EnqueueReplies(reply)
	temperature := 1.0
	result := runGenerateText(t, newClaudeClient(t, provider.BaseURL(), claude.Config{WorkspaceID: " " + testWorkspaceID + " "}), claude.GenerateTextRequest{
		Instructions: "Answer in one sentence.",
		Messages: []llm.Message{
			{Role: llm.MessageRoleUser, Text: "How does TCP differ from UDP?"},
			{Role: llm.MessageRoleAssistant, Text: "Do you want a short answer?"},
			{Role: llm.MessageRoleUser, Text: "Yes."},
		},
		StructuredOutput: &llm.StructuredOutput{Name: "answer", Description: "A short answer.", Schema: map[string]any{
			"type": "object", "properties": map[string]any{
				"answer":     map[string]any{"type": "string", "maxLength": 200},
				"confidence": map[string]any{"type": "integer", "minimum": 0, "maximum": 10},
			},
			"required": []any{"answer", "confidence"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature, ReasoningEffort: llm.ReasoningEffortExtraHigh,
	})

	require.Equal(t, claude.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, `{"answer":"TCP is reliable.","confidence":9}`, result.Value.Text)
	require.Equal(t, "claude-sonnet-5", result.Value.RequestedModel)
	require.Equal(t, "end_turn", result.Value.ProviderFinishReason)
	require.Equal(t, llm.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44}, result.Value.Usage)
	require.Equal(t, "claude", result.Receipt.Provider)
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
	require.Equal(t, testWorkspaceID, requests[0].Header.Get("anthropic-workspace-id"))
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

func TestGenerateTextSendsTheMinimalMessagesRequest(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, claudeDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(claudeDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Hello.", ServedModel: "claude-sonnet-5"}))
	result := runGenerateText(t, newClaudeClient(t, provider.BaseURL(), claude.Config{}), userRequest("Hello"))
	require.Equal(t, claude.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "Hello.", result.Value.Text, "thinking never reaches the text")
	requests := provider.Requests()
	require.Empty(t, requests[0].Header.Values("anthropic-workspace-id"), "a blank workspaceId sends no header")
	require.JSONEq(t, `{"model":"claude-sonnet-5","max_tokens":16000,"messages":[{"role":"user","content":"Hello"}],"stream":true}`,
		string(requests[0].Body), "Claude requires max_tokens, so the connection default applies")
}

// TestDefaultMaxOutputTokensFitsOneExchange keeps a full-length default generation inside the 870-second
// request timeout at the 128,000 output tokens per hour Anthropic's SDKs estimate, so it ends as truncated
// instead of timing out and retrying.
func TestDefaultMaxOutputTokensFitsOneExchange(t *testing.T) {
	const requestTimeout, sdkEstimatedTokensPerHour = 870 * time.Second, 128000
	expected := time.Duration(claude.DefaultConfig().DefaultMaxOutputTokens) * time.Hour / sdkEstimatedTokensPerHour
	require.Less(t, expected, requestTimeout*3/5, "the default leaves headroom for queueing and slower models")
	require.Equal(t, 900*time.Second, claude.GenerateTextDefinition.StepDefaults.ExecuteMethodTimeout)

	_, err := claude.New(claude.Config{}, testCredentials(), claude.WithHTTPClient(&http.Client{Timeout: 900 * time.Second}))
	require.ErrorContains(t, err, "Execute timeout", "an exchange cannot outlast the Execute timeout")
	_, err = claude.New(claude.Config{}, testCredentials(), claude.WithHTTPClient(&http.Client{Timeout: 600 * time.Second}))
	require.NoError(t, err, "a shorter exchange is accepted")
}

func TestGenerateTextAppliesThePerModelRules(t *testing.T) {
	temperature := 0.3
	for _, testCase := range []struct {
		name, model        string
		request            claude.GenerateTextRequest
		expectedBodyFields map[string]any
	}{
		{"custom default output limit", "claude-opus-5-5", claude.GenerateTextRequest{}, map[string]any{"max_tokens": float64(1000)}},
		{"max effort on Claude Opus 5.5", "claude-opus-5-5", claude.GenerateTextRequest{ReasoningEffort: llm.ReasoningEffortMax},
			map[string]any{"output_config": map[string]any{"effort": "max"}}},
		{"temperature on Claude Sonnet 4.6", "claude-sonnet-4-6", claude.GenerateTextRequest{Temperature: &temperature},
			map[string]any{"temperature": 0.3}},
		{"max effort on Claude Sonnet 4.6", "claude-sonnet-4-6", claude.GenerateTextRequest{ReasoningEffort: llm.ReasoningEffortMax},
			map[string]any{"output_config": map[string]any{"effort": "max"}}},
		{"temperature on dated Claude Haiku 4.5", "claude-haiku-4-5-20251001", claude.GenerateTextRequest{Temperature: &temperature},
			map[string]any{"temperature": 0.3}},
		{"high effort on Claude Opus 4.5", "claude-opus-4-5", claude.GenerateTextRequest{ReasoningEffort: llm.ReasoningEffortHigh},
			map[string]any{"output_config": map[string]any{"effort": "high"}}},
		{"xhigh effort on a model this release does not name", "claude-sonnet-5-1",
			claude.GenerateTextRequest{ReasoningEffort: llm.ReasoningEffortExtraHigh},
			map[string]any{"output_config": map[string]any{"effort": "xhigh"}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, claudeDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(claudeDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Done.", ServedModel: testCase.model}))
			request := testCase.request
			request.Model, request.Messages = testCase.model, userRequest("Plan the migration.").Messages
			result := runGenerateText(t, newClaudeClient(t, provider.BaseURL(), claude.Config{DefaultMaxOutputTokens: 1000}), request)
			require.Equal(t, claude.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			var body map[string]any
			require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
			for field, value := range testCase.expectedBodyFields {
				require.Equal(t, value, body[field], field)
			}
		})
	}
}

// TestStreamOutcomesFollowClaudeStopReasons covers the stop reasons and stream shapes the shared suite does not.
func TestStreamOutcomesFollowClaudeStopReasons(t *testing.T) {
	message := llmtest.GeneratedReply{ServedModel: "claude-sonnet-5", ResponseID: "msg_claude_2", Usage: llm.Usage{InputTokens: 9, OutputTokens: 3}}
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
			claude.GenerateTextBranchTruncated, sdkgo.FailureResponseTooLarge, "Partial"},
		{"a refusal after streamed text returns no text", messagestest.MessageStartEvent(message) + textBlockStart +
			messagestest.TextDeltaEvent(0, "I can") + stop("refusal"),
			claude.GenerateTextBranchBlocked, sdkgo.FailureProviderRejection, ""},
		{"tool_use without tools is unusable", messagestest.MessageStartEvent(message) + textBlockStart +
			messagestest.TextDeltaEvent(0, "Calling") + stop("tool_use"),
			claude.GenerateTextBranchInvalidResponse, sdkgo.FailureProtocol, ""},
		{"a text delta for a thinking block is malformed", messagestest.MessageStartEvent(message) +
			messagestest.Event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "thinking", "thinking": ""}}) +
			messagestest.TextDeltaEvent(0, "Leaked") + stop("end_turn"),
			claude.GenerateTextBranchInvalidResponse, sdkgo.FailureProtocol, ""},
		{"content before message_start is malformed", textBlockStart + messagestest.TextDeltaEvent(0, "Early") + stop("end_turn"),
			claude.GenerateTextBranchInvalidResponse, sdkgo.FailureProtocol, ""},
		{"unknown event types are ignored", messagestest.MessageStartEvent(message) +
			messagestest.Event("llmtest_future_event", map[string]any{"detail": "ignored"}) + textBlockStart +
			messagestest.TextDeltaEvent(0, "Done.") + stop("end_turn"),
			claude.GenerateTextBranchGenerated, "", "Done."},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, claudeDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(messagestest.EventStreamReply(testCase.stream))
			result := runGenerateText(t, newClaudeClient(t, provider.BaseURL(), claude.Config{}), userRequest("Hello"))
			require.Equal(t, testCase.branch, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, testCase.text, result.Value.Text)
			if testCase.kind != "" {
				require.Equal(t, testCase.kind, result.Failure.Kind)
			}
		})
	}
}

// TestMidStreamErrorEventsAreClassified follows Claude's error types for an error event after a 200 status.
func TestMidStreamErrorEventsAreClassified(t *testing.T) {
	message := llmtest.GeneratedReply{ServedModel: "claude-sonnet-5", ResponseID: "msg_claude_3"}
	partialStream := messagestest.MessageStartEvent(message) +
		messagestest.Event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "text", "text": ""}}) +
		messagestest.TextDeltaEvent(0, "Partial")
	for _, errorType := range []string{"overloaded_error", "api_error", "timeout_error"} {
		t.Run(errorType, func(t *testing.T) {
			requireRetry(t, messagestest.EventStreamReply(partialStream+messagestest.ErrorEvent(errorType, "Overloaded")), sdkgo.FailureAvailability)
		})
	}
	t.Run("rate_limit_error", func(t *testing.T) {
		requireRetry(t, messagestest.EventStreamReply(partialStream+messagestest.ErrorEvent("rate_limit_error", "Slow down")), sdkgo.FailureRateLimit)
	})
	t.Run("billing_error is quota", func(t *testing.T) {
		provider := llmtest.NewFakeProvider(t, claudeDialect.CredentialHeader, testAPIKey)
		provider.EnqueueReplies(messagestest.EventStreamReply(partialStream + messagestest.ErrorEvent("billing_error", "Pay message-canary")))
		result := runGenerateText(t, newClaudeClient(t, provider.BaseURL(), claude.Config{}), userRequest("Hello"))
		require.Equal(t, claude.GenerateTextBranchProviderRejected, result.Branch, "failure: %+v", result.Failure)
		require.Equal(t, sdkgo.FailureQuotaExhausted, result.Failure.Kind)
		require.NotContains(t, result.Failure.Message, "message-canary", "a Failure never carries provider message text")
		require.Empty(t, result.Value.Text)
	})
	t.Run("invalid_request_error is unusable", func(t *testing.T) {
		provider := llmtest.NewFakeProvider(t, claudeDialect.CredentialHeader, testAPIKey)
		provider.EnqueueReplies(messagestest.EventStreamReply(partialStream + messagestest.ErrorEvent("invalid_request_error", "Bad")))
		result := runGenerateText(t, newClaudeClient(t, provider.BaseURL(), claude.Config{}), userRequest("Hello"))
		require.Equal(t, claude.GenerateTextBranchInvalidResponse, result.Branch)
		require.Contains(t, result.Failure.Message, "invalid_request_error")
		require.Empty(t, result.Value.Text)
	})
}

// TestUnstreamedJSONBodiesAreClassified covers a gateway that answers the streaming request with a complete
// 2xx JSON body, which is classified instead of being retried as an interrupted stream.
func TestUnstreamedJSONBodiesAreClassified(t *testing.T) {
	t.Run("a 200 overloaded_error envelope returns Retry", func(t *testing.T) {
		requireRetry(t, messagestest.ErrorBodyReply(http.StatusOK, "overloaded_error", "Overloaded", ""), sdkgo.FailureAvailability)
	})
	t.Run("a 200 spend-cap envelope is quota", func(t *testing.T) {
		provider := llmtest.NewFakeProvider(t, claudeDialect.CredentialHeader, testAPIKey)
		provider.EnqueueReplies(messagestest.ErrorBodyReply(http.StatusOK, "rate_limit_error", "Cap message-canary", messagestest.SpendLimitErrorCode))
		result := runGenerateText(t, newClaudeClient(t, provider.BaseURL(), claude.Config{}), userRequest("Hello"))
		require.Equal(t, claude.GenerateTextBranchProviderRejected, result.Branch, "failure: %+v", result.Failure)
		require.Equal(t, sdkgo.FailureQuotaExhausted, result.Failure.Kind)
		require.NotContains(t, result.Failure.Message, "message-canary", "a Failure never carries provider message text")
	})
	for _, testCase := range []struct{ name, body string }{
		{"a 200 body of another type is unusable", `{"type":"completion","completion":"message-canary"}`},
		{"a 200 body that is not JSON is unusable", `message-canary`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, claudeDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(llmtest.FakeReply{Header: http.Header{"Content-Type": {"application/json"}}, Body: testCase.body})
			result := runGenerateText(t, newClaudeClient(t, provider.BaseURL(), claude.Config{}), userRequest("Hello"))
			require.Equal(t, claude.GenerateTextBranchInvalidResponse, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "message-canary", "a Failure never carries provider body text")
			require.Empty(t, result.Value.Text)
		})
	}
}

// TestErrorStatusesFollowClaudeErrorTypes covers the documented statuses the shared suite does not.
func TestErrorStatusesFollowClaudeErrorTypes(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		reply llmtest.FakeReply
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
			provider := llmtest.NewFakeProvider(t, claudeDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(testCase.reply)
			result := runGenerateText(t, newClaudeClient(t, provider.BaseURL(), claude.Config{}), userRequest("Hello"))
			require.Equal(t, claude.GenerateTextBranchProviderRejected, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "message-canary", "a Failure never carries provider message text")
		})
	}
	t.Run("529 overloaded_error", func(t *testing.T) {
		requireRetry(t, messagestest.ErrorReply(529, "Overloaded"), sdkgo.FailureAvailability)
	})
}

// TestGenerateTextCallsTheClaudeAPIHost proves the production URL without a network call.
func TestGenerateTextCallsTheClaudeAPIHost(t *testing.T) {
	reply := claudeDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Hello.", ServedModel: "claude-sonnet-5"})
	var requestedURLs []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestedURLs = append(requestedURLs, request.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK, Header: reply.Header.Clone(), Body: io.NopCloser(strings.NewReader(reply.Body)), Request: request,
		}, nil
	})
	client, err := claude.New(claude.Config{}, testCredentials(), claude.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	result := runGenerateText(t, client, userRequest("Hello"))
	require.Equal(t, claude.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "claude-sonnet-5", result.Value.RequestedModel, "a blank connection model uses the manifest default")
	require.Equal(t, []string{"https://api.anthropic.com/v1/messages"}, requestedURLs)
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	for _, baseURL := range []string{"https://api.anthropic.com.example.com", "http://192.0.2.10:8080", "https://attacker.example"} {
		_, err := claude.New(claude.Config{}, testCredentials(), claude.WithBaseURLForTest(baseURL))
		require.ErrorContains(t, err, "loopback", baseURL)
	}
	for _, baseURL := range []string{"http://127.0.0.1:8080", "http://localhost:9000", "http://[::1]:7000"} {
		_, err := claude.New(claude.Config{}, testCredentials(), claude.WithBaseURLForTest(baseURL))
		require.NoError(t, err, baseURL)
	}
	_, err := claude.New(claude.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = claude.New(claude.Config{Model: "claude-é"}, testCredentials())
	require.Error(t, err, "an invalid connection model fails at startup")
	for _, workspaceID := range []string{"workspace-1", "wrkspc_bad!", "wrkspc_01 space", "wrkspc_01\r\nX-Injected: 1"} {
		_, err = claude.New(claude.Config{WorkspaceID: workspaceID}, testCredentials())
		require.ErrorContains(t, err, "workspaceId", "%q", workspaceID)
		require.NotContains(t, err.Error(), workspaceID, "the error never repeats the value")
	}
	_, err = claude.New(claude.Config{DefaultMaxOutputTokens: -1}, testCredentials())
	require.ErrorContains(t, err, "defaultMaxOutputTokens")
	_, err = claude.New(claude.Config{MaxResponseBytes: -1}, testCredentials())
	require.ErrorContains(t, err, "maxResponseBytes")
	_, err = claude.New(claude.Config{}, testCredentials(), nil)
	require.ErrorContains(t, err, "option is nil")
}

func newClaudeQuery(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
	return newFakeProviderClient(t, connection).GenerateText()
}

// newFakeProviderClient builds a Client for an llmtest suite's fake provider and connection.
func newFakeProviderClient(t testing.TB, connection llmtest.FakeConnection) *claude.Client {
	t.Helper()
	client, err := claude.New(claude.Config{Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[claude.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		claude.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newClaudeClient(t *testing.T, baseURL string, config claude.Config) *claude.Client {
	t.Helper()
	client, err := claude.New(config, testCredentials(), claude.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func testCredentials() sdkgo.StaticCredentialProvider[claude.Credentials] {
	return sdkgo.StaticCredentialProvider[claude.Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
}

func runGenerateText(t *testing.T, client *claude.Client, request claude.GenerateTextRequest) claude.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("claude-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("claude-flow", stepExecutionID), client.GenerateText(), testConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), testAPIKey), "a Result contains the API key")
	return result
}

// requireRetry runs one request against reply and requires Retry with kind and no provider message text.
func requireRetry(t *testing.T, reply llmtest.FakeReply, kind sdkgo.FailureKind) {
	t.Helper()
	provider := llmtest.NewFakeProvider(t, claudeDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(reply)
	_, err := sdkgo.RunQuery(testsupport.NewDexContext("claude-flow", fmt.Sprintf("claude-retry-%d", time.Now().UnixNano())),
		newClaudeClient(t, provider.BaseURL(), claude.Config{}).GenerateText(), testConnection, userRequest("Hello"))
	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError)
	require.Equal(t, kind, retryError.Failure.Kind, "failure message: %s", retryError.Failure.Message)
	require.NotContains(t, retryError.Failure.Message, "Overloaded", "a Failure never carries provider message text")
	require.NotContains(t, err.Error(), testAPIKey)
}

func userRequest(text string) claude.GenerateTextRequest {
	return claude.GenerateTextRequest{Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: text}}}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
