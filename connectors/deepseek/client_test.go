// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package deepseek_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/deepseek"
	"github.com/superdurable/dex-connectors-library/connectors/deepseek/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat/openaichattest"
)

const testAPIKey = "sk-deepseek-connector-test-key-0123456789"

var testConnection = sdkgo.ConnectionRef{Provider: "deepseek", Name: "deepseek-test"}

// deepSeekDialect matches chatProfile: streamed replies, the default 402 balance error, and the x-ds-trace-id header.
var deepSeekDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "deepseek-flash", AlternateModel: "deepseek-v4-pro", IsStreaming: true,
	RequestIDHeader: "x-ds-trace-id",
})

func TestGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperature := 0.7
	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
		Dialect:  deepSeekDialect,
		NewQuery: newDeepSeekQuery,
		LocallyRejectedRequests: []llmtest.NamedTextGenerationRequest{
			{Name: "temperature, which thinking mode ignores", Request: llm.TextGenerationRequest{Temperature: &temperature}},
			{Name: "temperature with thinking disabled", Request: llm.TextGenerationRequest{
				Temperature: &temperature, ReasoningEffort: llm.ReasoningEffortNone,
			}},
			{Name: "minimal reasoning effort, which DeepSeek runs as low", Request: llm.TextGenerationRequest{
				ReasoningEffort: llm.ReasoningEffortMinimal,
			}},
			{Name: "medium reasoning effort, which DeepSeek runs as high", Request: llm.TextGenerationRequest{
				ReasoningEffort: llm.ReasoningEffortMedium,
			}},
			{Name: "xhigh reasoning effort, which DeepSeek runs as high", Request: llm.TextGenerationRequest{
				Model: "deepseek-v4-pro", ReasoningEffort: llm.ReasoningEffortExtraHigh,
			}},
		},
	})
}

// TestGenerateTextSendsTheDocumentedChatCompletionsRequest pins the body DeepSeek documents and decodes its thinking stream.
func TestGenerateTextSendsTheDocumentedChatCompletionsRequest(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, deepSeekDialect.CredentialHeader, testAPIKey)
	reply := deepSeekThinkingStream("stop", `{"answer":`, `"TCP is reliable."}`)
	reply.Header.Set("x-ds-trace-id", "a2208ecfd95f5cbc3d78850b1700f37b")
	provider.EnqueueReplies(reply)
	schema := map[string]any{
		"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
		"required": []any{"answer"}, "additionalProperties": false,
	}
	result := runGenerateText(t, newDeepSeekClient(t, provider.BaseURL(), ""), deepseek.GenerateTextRequest{
		Instructions:     "Answer in one sentence.",
		Messages:         []llm.Message{{Role: llm.MessageRoleUser, Text: "How does TCP differ from UDP?"}},
		StructuredOutput: &llm.StructuredOutput{Name: "answer", Schema: schema},
		MaxOutputTokens:  2048, ReasoningEffort: llm.ReasoningEffortMax,
	})

	require.Equal(t, deepseek.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, `{"answer":"TCP is reliable."}`, result.Value.Text, "reasoning_content never reaches the text")
	require.Equal(t, "deepseek-flash", result.Value.RequestedModel)
	require.Equal(t, "deepseek-flash", result.Value.ServedModel)
	require.Equal(t, "8f2a4c0e-deepseek-stream", result.Value.ResponseID)
	require.Equal(t, llm.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44},
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

// TestGenerateTextDisablesThinkingWithReasoningEffortNone sends the one Chat Completions value that turns thinking mode off.
func TestGenerateTextDisablesThinkingWithReasoningEffortNone(t *testing.T) {
	for _, testCase := range []struct {
		effort    llm.ReasoningEffort
		wireValue string
	}{
		{llm.ReasoningEffortNone, "none"}, {llm.ReasoningEffortLow, "low"}, {llm.ReasoningEffortHigh, "high"},
	} {
		t.Run(testCase.wireValue, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, deepSeekDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(deepSeekThinkingStream("stop", "Done."))
			request := userRequest("Plan the migration.")
			request.ReasoningEffort = testCase.effort
			result := runGenerateText(t, newDeepSeekClient(t, provider.BaseURL(), "deepseek-v4-pro"), request)
			require.Equal(t, deepseek.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, "deepseek-v4-pro", result.Value.RequestedModel)
			var body struct {
				ReasoningEffort string `json:"reasoning_effort"`
			}
			require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
			require.Equal(t, testCase.wireValue, body.ReasoningEffort)
		})
	}
}

// TestInterruptedGenerationFinishReasonsSelectInvalidResponse keeps DeepSeek's interruption tokens off the generated branch.
func TestInterruptedGenerationFinishReasonsSelectInvalidResponse(t *testing.T) {
	for _, finishReason := range []string{"insufficient_system_resource", "aborted"} {
		t.Run(finishReason, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, deepSeekDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(deepSeekThinkingStream(finishReason, "Partial"))
			result := runGenerateText(t, newDeepSeekClient(t, provider.BaseURL(), ""), userRequest("Hello"))
			require.Equal(t, deepseek.GenerateTextBranchInvalidResponse, result.Branch)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, finishReason)
			require.Equal(t, finishReason, result.Value.ProviderFinishReason)
			require.Empty(t, result.Value.Text)
		})
	}
}

// TestEmptyJSONOutputSelectsInvalidResponse covers the empty content DeepSeek documents that JSON Output occasionally returns.
func TestEmptyJSONOutputSelectsInvalidResponse(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, deepSeekDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(deepSeekThinkingStream("stop"))
	request := userRequest("Reply with an object.")
	request.StructuredOutput = &llm.StructuredOutput{Name: "probe", Schema: map[string]any{
		"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
		"required": []any{"ok"}, "additionalProperties": false,
	}}
	result := runGenerateText(t, newDeepSeekClient(t, provider.BaseURL(), ""), request)
	require.Equal(t, deepseek.GenerateTextBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
}

// TestDocumentedErrorStatusesUseDeepSeekErrorEnvelope classifies the error codes DeepSeek documents in its own envelope.
func TestDocumentedErrorStatusesUseDeepSeekErrorEnvelope(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		statusCode      int
		errorType, code string
		branch          sdkgo.BranchID
		kind            sdkgo.FailureKind
	}{
		{"400 invalid format", http.StatusBadRequest, "invalid_request_error", "invalid_request_error", deepseek.GenerateTextBranchProviderRejected, sdkgo.FailureProviderRejection},
		{"401 authentication fails", http.StatusUnauthorized, "authentication_error", "invalid_request_error", deepseek.GenerateTextBranchProviderRejected, sdkgo.FailureAuthentication},
		{"402 insufficient balance", http.StatusPaymentRequired, "unknown_error", "invalid_request_error", deepseek.GenerateTextBranchProviderRejected, sdkgo.FailureQuotaExhausted},
		{"422 invalid parameters", http.StatusUnprocessableEntity, "invalid_request_error", "invalid_request_error", deepseek.GenerateTextBranchProviderRejected, sdkgo.FailureProviderRejection},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, deepSeekDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(deepSeekErrorReply(testCase.statusCode, testCase.errorType, testCase.code))
			result := runGenerateText(t, newDeepSeekClient(t, provider.BaseURL(), ""), userRequest("Hello"))
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
			provider := llmtest.NewFakeProvider(t, deepSeekDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(deepSeekErrorReply(testCase.statusCode, "unknown_error", "unknown_error"))
			_, err := sdkgo.RunQuery(testsupport.NewDexContext("deepseek-flow", "deepseek-retry-"+testCase.name),
				newDeepSeekClient(t, provider.BaseURL(), "").GenerateText(), testConnection, userRequest("Hello"))
			var retryError *sdkgo.RetryError
			require.ErrorAs(t, err, &retryError)
			require.Equal(t, testCase.kind, retryError.Failure.Kind)
		})
	}
}

// TestGenerateTextCallsTheDeepSeekAPIHost proves the production URL, without /v1, and the default model without a network call.
func TestGenerateTextCallsTheDeepSeekAPIHost(t *testing.T) {
	reply := deepSeekThinkingStream("stop", "Hello.")
	var requestedURLs []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestedURLs = append(requestedURLs, request.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK, Header: reply.Header.Clone(), Body: io.NopCloser(strings.NewReader(reply.Body)), Request: request,
		}, nil
	})
	client, err := deepseek.New(deepseek.Config{}, testCredentials(), deepseek.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	result := runGenerateText(t, client, userRequest("Hello"))
	require.Equal(t, deepseek.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "deepseek-flash", result.Value.RequestedModel, "a blank connection model uses the manifest default")
	require.Equal(t, []string{"https://api.deepseek.com/chat/completions"}, requestedURLs)
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	for _, baseURL := range []string{"https://api.deepseek.com.example.com", "http://192.0.2.10:8080", "https://attacker.example"} {
		_, err := deepseek.New(deepseek.Config{}, testCredentials(), deepseek.WithBaseURLForTest(baseURL))
		require.ErrorContains(t, err, "loopback", baseURL)
	}
	for _, baseURL := range []string{"http://127.0.0.1:8080", "http://localhost:9000", "http://[::1]:7000"} {
		_, err := deepseek.New(deepseek.Config{}, testCredentials(), deepseek.WithBaseURLForTest(baseURL))
		require.NoError(t, err, baseURL)
	}
	_, err := deepseek.New(deepseek.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = deepseek.New(deepseek.Config{Model: "deepseek-é"}, testCredentials())
	require.Error(t, err, "an invalid connection model fails at startup")
	_, err = deepseek.New(deepseek.Config{MaxResponseBytes: -1}, testCredentials())
	require.ErrorContains(t, err, "maxResponseBytes")
	_, err = deepseek.New(deepseek.Config{}, testCredentials(), nil)
	require.ErrorContains(t, err, "option is nil")
	_, err = deepseek.New(deepseek.Config{}, testCredentials(), deepseek.WithHTTPClient(&http.Client{Timeout: time.Hour}))
	require.ErrorContains(t, err, "Execute timeout must exceed the request timeout")
}

func TestNewUsesTheDefaultModelForAWhitespaceConnectionModel(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, deepSeekDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(deepSeekThinkingStream("stop", "Hello."))
	result := runGenerateText(t, newDeepSeekClient(t, provider.BaseURL(), " \t "), userRequest("Hello"))
	require.Equal(t, deepseek.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "deepseek-flash", result.Value.RequestedModel)
}

func newDeepSeekQuery(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
	return newFakeProviderClient(t, connection).GenerateText()
}

// newFakeProviderClient builds a Client for an llmtest suite's fake provider and connection.
func newFakeProviderClient(t testing.TB, connection llmtest.FakeConnection) *deepseek.Client {
	t.Helper()
	client, err := deepseek.New(deepseek.Config{Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[deepseek.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		deepseek.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newDeepSeekClient(t *testing.T, baseURL string, model string) *deepseek.Client {
	t.Helper()
	client, err := deepseek.New(deepseek.Config{Model: model}, testCredentials(), deepseek.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func testCredentials() sdkgo.StaticCredentialProvider[deepseek.Credentials] {
	return sdkgo.StaticCredentialProvider[deepseek.Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
}

func runGenerateText(t *testing.T, client *deepseek.Client, request deepseek.GenerateTextRequest) deepseek.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("deepseek-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("deepseek-flow", stepExecutionID), client.GenerateText(), testConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), testAPIKey), "a Result contains the API key")
	return result
}

func userRequest(text string) deepseek.GenerateTextRequest {
	return deepseek.GenerateTextRequest{Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: text}}}
}

// deepSeekThinkingStream renders the documented stream: queue keep-alive comments, reasoning_content deltas,
// content deltas, and a finish chunk that carries usage.
func deepSeekThinkingStream(finishReason string, contentDeltas ...string) llmtest.FakeReply {
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
	return llmtest.FakeReply{Header: http.Header{"Content-Type": {"text/event-stream; charset=utf-8"}}, Body: stream.String()}
}

// deepSeekErrorReply renders the error envelope api.deepseek.com returns, with a message that must stay out of Failures.
func deepSeekErrorReply(statusCode int, errorType string, code string) llmtest.FakeReply {
	body, err := json.Marshal(map[string]any{"error": map[string]any{
		"message": "Authentication Fails, Your api key: ****6789 is invalid", "type": errorType, "param": nil, "code": code,
	}})
	if err != nil {
		panic(err)
	}
	return llmtest.FakeReply{StatusCode: statusCode, Header: http.Header{"Content-Type": {"application/json"}}, Body: string(body)}
}

func bodyFieldNames(fields map[string]json.RawMessage) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	return names
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
