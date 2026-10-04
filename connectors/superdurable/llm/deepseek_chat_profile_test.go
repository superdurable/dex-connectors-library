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

const deepSeekTestAPIKey = "sk-deepseek-connector-test-key-0123456789"

var deepSeekTestConnection = sdkgo.ConnectionRef{Provider: "llm", Name: "deepseek-test"}

// deepSeekDialect matches chatProfile: streamed replies, the default 402 balance error, and the x-ds-trace-id header.
var deepSeekDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "deepseek-flash", AlternateModel: "deepseek-v4-pro", IsStreaming: true,
	RequestIDHeader: "x-ds-trace-id",
})

func TestDeepSeekGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperature := 0.7
	textgentest.RunTextGenerationExchangeSuite(t, &textgentest.TextGenerationExchangeSuite{
		Dialect:  deepSeekDialect,
		NewQuery: newDeepSeekQuery,
		LocallyRejectedRequests: []textgentest.NamedTextGenerationRequest{
			{Name: "temperature, which thinking mode ignores", Request: textgen.TextGenerationRequest{Temperature: &temperature}},
			{Name: "temperature with thinking disabled", Request: textgen.TextGenerationRequest{
				Temperature: &temperature, ReasoningEffort: textgen.ReasoningEffortNone,
			}},
			{Name: "minimal reasoning effort, which DeepSeek runs as low", Request: textgen.TextGenerationRequest{
				ReasoningEffort: textgen.ReasoningEffortMinimal,
			}},
			{Name: "medium reasoning effort, which DeepSeek runs as high", Request: textgen.TextGenerationRequest{
				ReasoningEffort: textgen.ReasoningEffortMedium,
			}},
			{Name: "xhigh reasoning effort, which DeepSeek runs as high", Request: textgen.TextGenerationRequest{
				Model: "deepseek-v4-pro", ReasoningEffort: textgen.ReasoningEffortExtraHigh,
			}},
		},
	})
}

// TestDeepSeekGenerateTextSendsTheDocumentedChatCompletionsRequest pins the body DeepSeek documents and decodes its thinking stream.
func TestDeepSeekGenerateTextSendsTheDocumentedChatCompletionsRequest(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, deepSeekDialect.CredentialHeader, deepSeekTestAPIKey)
	reply := deepSeekThinkingStream("stop", `{"answer":`, `"TCP is reliable."}`)
	reply.Header.Set("x-ds-trace-id", "a2208ecfd95f5cbc3d78850b1700f37b")
	provider.EnqueueReplies(reply)
	schema := map[string]any{
		"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
		"required": []any{"answer"}, "additionalProperties": false,
	}
	result := runDeepSeekGenerateText(t, newDeepSeekClient(t, provider.BaseURL(), ""), llm.GenerateTextRequest{
		Instructions:     "Answer in one sentence.",
		Messages:         []textgen.Message{{Role: textgen.MessageRoleUser, Text: "How does TCP differ from UDP?"}},
		StructuredOutput: &textgen.StructuredOutput{Name: "answer", Schema: schema},
		MaxOutputTokens:  2048, ReasoningEffort: textgen.ReasoningEffortMax,
	})

	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, `{"answer":"TCP is reliable."}`, result.Value.Text, "reasoning_content never reaches the text")
	require.Equal(t, "deepseek-flash", result.Value.RequestedModel)
	require.Equal(t, "deepseek-flash", result.Value.ServedModel)
	require.Equal(t, "8f2a4c0e-deepseek-stream", result.Value.ResponseID)
	require.Equal(t, textgen.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44},
		result.Value.Usage, "usage rides on the finish chunk without stream_options")
	require.Equal(t, "deepseek", result.Receipt.Provider)
	require.Equal(t, "a2208ecfd95f5cbc3d78850b1700f37b", result.Receipt.ProviderRequestID)
	requests := provider.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].Method)
	require.Equal(t, "/chat/completions", requests[0].Path)
	require.Equal(t, "text/event-stream", requests[0].Header.Get("Accept"))
	require.True(t, requests[0].HasCredentialInSlot, "the key travels as an Authorization bearer token")
	var body struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		MaxTokens       int            `json:"max_tokens"`
		ReasoningEffort string         `json:"reasoning_effort"`
		ResponseFormat  map[string]any `json:"response_format"`
		Stream          bool           `json:"stream"`
	}
	require.NoError(t, json.Unmarshal(requests[0].Body, &body))
	require.Equal(t, "deepseek-flash", body.Model)
	require.Len(t, body.Messages, 2)
	require.Equal(t, "system", body.Messages[0].Role)
	require.True(t, strings.HasPrefix(body.Messages[0].Content, "Answer in one sentence.\n\n"), "the JSON instruction follows the instructions")
	require.Contains(t, body.Messages[0].Content, "JSON", "DeepSeek requires the prompt to ask for JSON")
	encodedSchema, err := json.Marshal(schema)
	require.NoError(t, err)
	require.Contains(t, body.Messages[0].Content, string(encodedSchema), "json_object mode carries the schema in the instructions")
	require.Equal(t, "user", body.Messages[1].Role)
	require.Equal(t, "How does TCP differ from UDP?", body.Messages[1].Content)
	require.Equal(t, 2048, body.MaxTokens)
	require.Equal(t, "max", body.ReasoningEffort)
	require.Equal(t, map[string]any{"type": "json_object"}, body.ResponseFormat, "DeepSeek accepts only text and json_object")
	require.True(t, body.Stream)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(requests[0].Body, &fields))
	require.ElementsMatch(t, []string{"model", "messages", "max_tokens", "reasoning_effort", "response_format", "stream"}, bodyFieldNames(fields),
		"no temperature, thinking, or stream_options field is sent")
}

// TestDeepSeekGenerateTextDisablesThinkingWithReasoningEffortNone sends the one Chat Completions value that turns thinking mode off.
func TestDeepSeekGenerateTextDisablesThinkingWithReasoningEffortNone(t *testing.T) {
	for _, testCase := range []struct {
		effort    textgen.ReasoningEffort
		wireValue string
	}{
		{textgen.ReasoningEffortNone, "none"}, {textgen.ReasoningEffortLow, "low"}, {textgen.ReasoningEffortHigh, "high"},
	} {
		t.Run(testCase.wireValue, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, deepSeekDialect.CredentialHeader, deepSeekTestAPIKey)
			provider.EnqueueReplies(deepSeekThinkingStream("stop", "Done."))
			request := deepSeekUserRequest("Plan the migration.")
			request.ReasoningEffort = testCase.effort
			result := runDeepSeekGenerateText(t, newDeepSeekClient(t, provider.BaseURL(), "deepseek-v4-pro"), request)
			require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, "deepseek-v4-pro", result.Value.RequestedModel)
			var body struct {
				ReasoningEffort string `json:"reasoning_effort"`
			}
			require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
			require.Equal(t, testCase.wireValue, body.ReasoningEffort)
		})
	}
}

// TestDeepSeekInterruptedGenerationFinishReasonsSelectInvalidResponse keeps DeepSeek's interruption tokens off the generated branch.
func TestDeepSeekInterruptedGenerationFinishReasonsSelectInvalidResponse(t *testing.T) {
	for _, finishReason := range []string{"insufficient_system_resource", "aborted"} {
		t.Run(finishReason, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, deepSeekDialect.CredentialHeader, deepSeekTestAPIKey)
			provider.EnqueueReplies(deepSeekThinkingStream(finishReason, "Partial"))
			result := runDeepSeekGenerateText(t, newDeepSeekClient(t, provider.BaseURL(), ""), deepSeekUserRequest("Hello"))
			require.Equal(t, llm.GenerateTextBranchInvalidResponse, result.Branch)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, finishReason)
			require.Equal(t, finishReason, result.Value.ProviderFinishReason)
			require.Empty(t, result.Value.Text)
		})
	}
}

// TestDeepSeekEmptyJSONOutputSelectsInvalidResponse covers the empty content DeepSeek documents that JSON Output occasionally returns.
func TestDeepSeekEmptyJSONOutputSelectsInvalidResponse(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, deepSeekDialect.CredentialHeader, deepSeekTestAPIKey)
	provider.EnqueueReplies(deepSeekThinkingStream("stop"))
	request := deepSeekUserRequest("Reply with an object.")
	request.StructuredOutput = &textgen.StructuredOutput{Name: "probe", Schema: map[string]any{
		"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
		"required": []any{"ok"}, "additionalProperties": false,
	}}
	result := runDeepSeekGenerateText(t, newDeepSeekClient(t, provider.BaseURL(), ""), request)
	require.Equal(t, llm.GenerateTextBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
}

// TestDeepSeekDocumentedErrorStatusesUseDeepSeekErrorEnvelope classifies the error codes DeepSeek documents in its own envelope.
func TestDeepSeekDocumentedErrorStatusesUseDeepSeekErrorEnvelope(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		statusCode      int
		errorType, code string
		branch          sdkgo.BranchID
		kind            sdkgo.FailureKind
	}{
		{"400 invalid format", http.StatusBadRequest, "invalid_request_error", "invalid_request_error", llm.GenerateTextBranchProviderRejected, sdkgo.FailureProviderRejection},
		{"401 authentication fails", http.StatusUnauthorized, "authentication_error", "invalid_request_error", llm.GenerateTextBranchProviderRejected, sdkgo.FailureAuthentication},
		{"402 insufficient balance", http.StatusPaymentRequired, "unknown_error", "invalid_request_error", llm.GenerateTextBranchProviderRejected, sdkgo.FailureQuotaExhausted},
		{"422 invalid parameters", http.StatusUnprocessableEntity, "invalid_request_error", "invalid_request_error", llm.GenerateTextBranchProviderRejected, sdkgo.FailureProviderRejection},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, deepSeekDialect.CredentialHeader, deepSeekTestAPIKey)
			provider.EnqueueReplies(deepSeekErrorReply(testCase.statusCode, testCase.errorType, testCase.code))
			result := runDeepSeekGenerateText(t, newDeepSeekClient(t, provider.BaseURL(), ""), deepSeekUserRequest("Hello"))
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, testCase.errorType)
			require.NotContains(t, result.Failure.Message, "Your api key", "a Failure never carries provider message text")
			require.Len(t, provider.Requests(), 1)
		})
	}
	for _, testCase := range []struct {
		name       string
		statusCode int
		kind       sdkgo.FailureKind
	}{
		{"429 rate limit reached", http.StatusTooManyRequests, sdkgo.FailureRateLimit},
		{"500 server error", http.StatusInternalServerError, sdkgo.FailureAvailability},
		{"503 server overloaded", http.StatusServiceUnavailable, sdkgo.FailureAvailability},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, deepSeekDialect.CredentialHeader, deepSeekTestAPIKey)
			provider.EnqueueReplies(deepSeekErrorReply(testCase.statusCode, "unknown_error", "unknown_error"))
			_, err := sdkgo.RunQuery(testsupport.NewDexContext("deepseek-flow", "deepseek-retry-"+testCase.name),
				newDeepSeekClient(t, provider.BaseURL(), "").GenerateText(), deepSeekTestConnection, deepSeekUserRequest("Hello"))
			var retryError *sdkgo.RetryError
			require.ErrorAs(t, err, &retryError)
			require.Equal(t, testCase.kind, retryError.Failure.Kind)
		})
	}
}

func newDeepSeekQuery(t testing.TB, connection textgentest.FakeConnection) *textgen.TextGenerationQuery {
	return newDeepSeekFakeProviderClient(t, connection).GenerateText()
}

// newDeepSeekFakeProviderClient builds a Client for a textgentest suite's fake provider and connection.
func newDeepSeekFakeProviderClient(t testing.TB, connection textgentest.FakeConnection) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderDeepseek, Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[llm.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		llm.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newDeepSeekClient(t *testing.T, baseURL string, model string) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderDeepseek, Model: model}, deepSeekTestCredentials(), llm.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func deepSeekTestCredentials() sdkgo.StaticCredentialProvider[llm.Credentials] {
	return sdkgo.StaticCredentialProvider[llm.Credentials]{deepSeekTestConnection: {APIKey: sdkgo.NewSecretString(deepSeekTestAPIKey)}}
}

func runDeepSeekGenerateText(t *testing.T, client *llm.Client, request llm.GenerateTextRequest) llm.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("deepseek-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("deepseek-flow", stepExecutionID), client.GenerateText(), deepSeekTestConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), deepSeekTestAPIKey), "a Result contains the API key")
	return result
}

func deepSeekUserRequest(text string) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: text}}}
}

// deepSeekThinkingStream renders the documented stream: queue keep-alive comments, reasoning_content deltas,
// content deltas, and a finish chunk that carries usage.
func deepSeekThinkingStream(finishReason string, contentDeltas ...string) textgentest.FakeReply {
	var stream strings.Builder
	stream.WriteString(": keep-alive\n\n: keep-alive\n\n")
	writeChunk := func(delta map[string]any, finishReason any, usage map[string]any) {
		chunk := map[string]any{
			"id": "8f2a4c0e-deepseek-stream", "object": "chat.completion.chunk", "created": 1790000000,
			"model": "deepseek-flash", "system_fingerprint": "fp_deepseek_test",
			"choices": []any{map[string]any{"index": 0, "delta": delta, "logprobs": nil, "finish_reason": finishReason}},
		}
		if usage != nil {
			chunk["usage"] = usage
		}
		encoded, err := json.Marshal(chunk)
		if err != nil {
			panic(err)
		}
		stream.WriteString("data: " + string(encoded) + "\n\n")
	}
	for _, reasoning := range []string{"The user wants", " a short answer."} {
		writeChunk(map[string]any{"role": "assistant", "content": nil, "reasoning_content": reasoning}, nil, nil)
	}
	for _, content := range contentDeltas {
		writeChunk(map[string]any{"role": "assistant", "content": content, "reasoning_content": nil}, nil, nil)
	}
	writeChunk(map[string]any{"content": "", "role": nil}, finishReason, map[string]any{
		"completion_tokens": 19, "prompt_tokens": 25, "total_tokens": 44,
		"prompt_tokens_details": map[string]any{"cached_tokens": 5}, "prompt_cache_hit_tokens": 5, "prompt_cache_miss_tokens": 20,
		"completion_tokens_details": map[string]any{"reasoning_tokens": 12},
	})
	stream.WriteString("data: [DONE]\n\n")
	return textgentest.FakeReply{Header: http.Header{"Content-Type": {"text/event-stream; charset=utf-8"}}, Body: stream.String()}
}

// deepSeekErrorReply renders the error envelope api.llm.com returns, with a message that must stay out of Failures.
func deepSeekErrorReply(statusCode int, errorType string, code string) textgentest.FakeReply {
	body, err := json.Marshal(map[string]any{"error": map[string]any{
		"message": "Authentication Fails, Your api key: ****6789 is invalid", "type": errorType, "param": nil, "code": code,
	}})
	if err != nil {
		panic(err)
	}
	return textgentest.FakeReply{StatusCode: statusCode, Header: http.Header{"Content-Type": {"application/json"}}, Body: string(body)}
}
