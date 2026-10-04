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
	"github.com/superdurable/dex/sdk-go/dex"
)

const mistralTestAPIKey = "mistral-connector-test-key-0123456789abcdef"

var mistralTestConnection = sdkgo.ConnectionRef{Provider: "llm", Name: "mistral-test"}

// mistralDialect matches chatProfile: streamed replies, the default 402 quota error, Mistral's top-level
// error envelope, and the mistral-correlation-id request ID.
var mistralDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "mistral-large-2512", AlternateModel: "mistral-small-2603", IsStreaming: true,
	RequestIDHeader: "mistral-correlation-id", ErrorTokenPointers: []string{"/type", "/code"},
})

func TestMistralGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange := 1.6
	textgentest.RunTextGenerationExchangeSuite(t, &textgentest.TextGenerationExchangeSuite{
		Dialect:  mistralDialect,
		NewQuery: newMistralQuery,
		LocallyRejectedRequests: []textgentest.NamedTextGenerationRequest{
			{Name: "temperature above 1.5", Request: textgen.TextGenerationRequest{Temperature: &temperatureAboveRange}},
			{Name: "reasoning effort on mistral-large-2512, which has no adjustable reasoning", Request: textgen.TextGenerationRequest{
				ReasoningEffort: textgen.ReasoningEffortHigh,
			}},
			{Name: "undocumented low effort on mistral-small-2603", Request: textgen.TextGenerationRequest{
				Model: "mistral-small-2603", ReasoningEffort: textgen.ReasoningEffortLow,
			}},
			{Name: "undocumented extra-high effort on mistral-medium-3-5", Request: textgen.TextGenerationRequest{
				Model: "mistral-medium-3-5", ReasoningEffort: textgen.ReasoningEffortExtraHigh,
			}},
			{Name: "reasoning effort on the deprecated mistral-medium-2508", Request: textgen.TextGenerationRequest{
				Model: "mistral-medium-2508", ReasoningEffort: textgen.ReasoningEffortNone,
			}},
			{Name: "reasoning effort on ministral-14b-2512", Request: textgen.TextGenerationRequest{
				Model: "ministral-14b-2512", ReasoningEffort: textgen.ReasoningEffortNone,
			}},
		},
	})
}

// TestMistralGenerateTextSendsTheDocumentedChatCompletionsRequest pins the body fields Mistral's ChatCompletionRequest accepts.
func TestMistralGenerateTextSendsTheDocumentedChatCompletionsRequest(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, mistralDialect.CredentialHeader, mistralTestAPIKey)
	reply := mistralDialect.GeneratedReply(textgentest.GeneratedReply{
		Text: `{"answer":"TCP is reliable."}`, ServedModel: "mistral-small-2603", ResponseID: "cmpl-mistral-1",
		Usage: textgen.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, TotalTokens: 44},
	})
	reply.Header.Set("mistral-correlation-id", "01a0e51d-7e0c-708a-bddd-bcdc94fe7152")
	reply.Header.Set("x-ratelimit-remaining", "59")
	provider.EnqueueReplies(reply)
	temperature := 0.3
	result := runMistralGenerateText(t, newMistralClient(t, provider.BaseURL(), ""), llm.GenerateTextRequest{
		Model:        "mistral-small-2603",
		Instructions: "Answer in one sentence.",
		Messages:     []textgen.Message{{Role: textgen.MessageRoleUser, Text: "How does TCP differ from UDP?"}},
		StructuredOutput: &textgen.StructuredOutput{Name: "answer", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
			"required": []any{"answer"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature, ReasoningEffort: textgen.ReasoningEffortHigh,
	})

	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, `{"answer":"TCP is reliable."}`, result.Value.Text)
	require.Equal(t, "mistral-small-2603", result.Value.RequestedModel)
	require.Equal(t, int64(5), result.Value.Usage.CachedInputTokens)
	require.Equal(t, "mistral", result.Receipt.Provider)
	require.Equal(t, "01a0e51d-7e0c-708a-bddd-bcdc94fe7152", result.Receipt.ProviderRequestID)
	require.Equal(t, map[string]string{"x-ratelimit-remaining": "59"}, result.Receipt.Metadata)
	requests := provider.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].Method)
	require.Equal(t, "/chat/completions", requests[0].Path)
	require.Equal(t, "text/event-stream", requests[0].Header.Get("Accept"))
	require.True(t, requests[0].HasCredentialInSlot, "the key travels as an Authorization bearer token")
	var body map[string]any
	require.NoError(t, json.Unmarshal(requests[0].Body, &body))
	require.Equal(t, map[string]any{
		"model": "mistral-small-2603",
		"messages": []any{
			map[string]any{"role": "system", "content": "Answer in one sentence."},
			map[string]any{"role": "user", "content": "How does TCP differ from UDP?"},
		},
		"max_tokens":       float64(2048),
		"temperature":      0.3,
		"reasoning_effort": "high",
		"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{
			"name": "answer", "strict": true, "schema": map[string]any{
				"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
				"required": []any{"answer"}, "additionalProperties": false,
			},
		}},
		"stream": true,
	}, body, "Mistral rejects unknown fields, so no stream_options or max_completion_tokens")
}

// TestMistralAdjustableReasoningModelsAcceptNoneAndHigh keeps each documented effort on each documented model ID and alias.
func TestMistralAdjustableReasoningModelsAcceptNoneAndHigh(t *testing.T) {
	for _, model := range []string{"mistral-small-2603", "mistral-small-latest", "mistral-medium-3-5", "mistral-medium-3", "mistral-medium-latest"} {
		for effort, wireValue := range map[textgen.ReasoningEffort]string{textgen.ReasoningEffortNone: "none", textgen.ReasoningEffortHigh: "high"} {
			t.Run(model+"/"+wireValue, func(t *testing.T) {
				provider := textgentest.NewFakeProvider(t, mistralDialect.CredentialHeader, mistralTestAPIKey)
				provider.EnqueueReplies(mistralDialect.GeneratedReply(textgentest.GeneratedReply{Text: "Done.", ServedModel: model}))
				result := runMistralGenerateText(t, newMistralClient(t, provider.BaseURL(), model), llm.GenerateTextRequest{
					Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: "Plan the migration."}}, ReasoningEffort: effort,
				})
				require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
				var body struct {
					ReasoningEffort string `json:"reasoning_effort"`
				}
				require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
				require.Equal(t, wireValue, body.ReasoningEffort)
			})
		}
	}
}

// TestMistralReasoningStreamKeepsOnlyTheAnswerText replays Mistral's documented thinking-chunk stream shape.
func TestMistralReasoningStreamKeepsOnlyTheAnswerText(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, mistralDialect.CredentialHeader, mistralTestAPIKey)
	chunk := func(delta string, finishReason string, usage string) string {
		finish := "null"
		if finishReason != "" {
			finish = `"` + finishReason + `"`
		}
		return fmt.Sprintf(`data: {"id":"cmpl-mistral-2","object":"chat.completion.chunk","created":1790000000,`+
			`"model":"mistral-medium-3-5","choices":[{"index":0,"delta":%s,"finish_reason":%s}],"usage":%s}`+"\n\n", delta, finish, usage)
	}
	provider.EnqueueReplies(textgentest.FakeReply{
		Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: chunk(`{"role":"assistant","content":[{"type":"thinking","thinking":[{"type":"text","text":"17 times 23 is 391."}]}]}`, "", "null") +
			chunk(`{"content":[{"type":"thinking","thinking":[]},{"type":"text","text":"17 * 23"}]}`, "", "null") +
			chunk(`{"content":" = 391."}`, "stop", `{"prompt_tokens":25,"completion_tokens":187,"total_tokens":212,"prompt_tokens_details":{"cached_tokens":0}}`) +
			"data: [DONE]\n\n",
	})
	result := runMistralGenerateText(t, newMistralClient(t, provider.BaseURL(), "mistral-medium-3-5"), llm.GenerateTextRequest{
		Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: "What is 17 * 23?"}}, ReasoningEffort: textgen.ReasoningEffortHigh,
	})
	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "17 * 23 = 391.", result.Value.Text, "the thinking trace never reaches the text")
	require.Equal(t, "mistral-medium-3-5", result.Value.ServedModel)
	require.Equal(t, textgen.Usage{InputTokens: 25, OutputTokens: 187, TotalTokens: 212}, result.Value.Usage)
}

// TestMistralStreamUsageArrivesWithTheFinishChunk replays the documented stream, whose last chunk has both finish_reason and usage.
func TestMistralStreamUsageArrivesWithTheFinishChunk(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, mistralDialect.CredentialHeader, mistralTestAPIKey)
	chunk := func(delta string, finishReason string, usage string) string {
		return `data: {"id":"59060ef9339a4112b2c9e57e3ee6199d","model":"mistral-large-latest","choices":[{"index":0,"delta":` + delta +
			`,"finish_reason":` + finishReason + `}],"object":"chat.completion.chunk","created":1764258570,"usage":` + usage + "}\n\n"
	}
	provider.EnqueueReplies(textgentest.FakeReply{
		Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: chunk(`{"role":"assistant","content":""}`, "null", "null") + chunk(`{"content":"3"}`, "null", "null") +
			chunk(`{"content":"84"}`, "null", "null") + chunk(`{"content":"4"}`, "null", "null") +
			chunk(`{"content":"00"}`, `"stop"`, `{"prompt_tokens":19,"completion_tokens":7,"total_tokens":26}`) + "data: [DONE]\n\n",
	})
	result := runMistralGenerateText(t, newMistralClient(t, provider.BaseURL(), "mistral-large-latest"), mistralUserRequest("How far is the moon?"))
	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "384400", result.Value.Text)
	require.Equal(t, "59060ef9339a4112b2c9e57e3ee6199d", result.Value.ResponseID)
	require.Equal(t, textgen.Usage{InputTokens: 19, OutputTokens: 7, TotalTokens: 26}, result.Value.Usage)
}

// TestMistralFinishReasonsFollowMistralsEnum maps model_length to truncated and keeps error and tool_calls invalid responses.
func TestMistralFinishReasonsFollowMistralsEnum(t *testing.T) {
	for _, testCase := range []struct {
		finishReason string
		branch       sdkgo.BranchID
		text         string
	}{
		{"model_length", llm.GenerateTextBranchTruncated, "Partial answer"},
		{"length", llm.GenerateTextBranchTruncated, "Partial answer"},
		{"error", llm.GenerateTextBranchInvalidResponse, ""},
		{"tool_calls", llm.GenerateTextBranchInvalidResponse, ""},
	} {
		t.Run(testCase.finishReason, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, mistralDialect.CredentialHeader, mistralTestAPIKey)
			provider.EnqueueReplies(textgentest.FakeReply{
				Header: http.Header{"Content-Type": {"text/event-stream"}},
				Body: `data: {"id":"cmpl-mistral-3","object":"chat.completion.chunk","model":"mistral-large-2512",` +
					`"choices":[{"index":0,"delta":{"role":"assistant","content":"Partial answer"},"finish_reason":"` + testCase.finishReason + `"}],` +
					`"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}}` + "\n\ndata: [DONE]\n\n",
			})
			result := runMistralGenerateText(t, newMistralClient(t, provider.BaseURL(), ""), mistralUserRequest("Hello"))
			require.Equal(t, testCase.branch, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, testCase.text, result.Value.Text)
			require.Equal(t, testCase.finishReason, result.Value.ProviderFinishReason)
		})
	}
}

// TestMistralErrorEnvelopeTokensReachTheFailure reads the documented top-level type and code, never the message.
func TestMistralErrorEnvelopeTokensReachTheFailure(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		statusCode int
		body       string
		kind       sdkgo.FailureKind
		tokens     []string
	}{
		{
			name: "documented envelope", statusCode: http.StatusBadRequest, kind: sdkgo.FailureProviderRejection,
			body:   `{"object":"error","message":"Invalid model: mistral-no-such-model","type":"invalid_request_error","param":"model","code":"unknown_model"}`,
			tokens: []string{"invalid_request_error", "unknown_model"},
		},
		{
			name: "retired model", statusCode: http.StatusNotFound, kind: sdkgo.FailureNotFound,
			body:   `{"object":"error","message":"Invalid model: mistral-no-such-model","type":"invalid_request_error","param":null,"code":null}`,
			tokens: []string{"invalid_request_error"},
		},
		{
			name: "gateway key rejection", statusCode: http.StatusUnauthorized, kind: sdkgo.FailureAuthentication,
			body: `{"detail":"Invalid API Key"}`,
		},
		{
			name: "request validation", statusCode: http.StatusUnprocessableEntity, kind: sdkgo.FailureProviderRejection,
			body: `{"detail":[{"type":"extra_forbidden","loc":["body","mistral-no-such-model"],"msg":"Extra inputs are not permitted"}]}`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, mistralDialect.CredentialHeader, mistralTestAPIKey)
			provider.EnqueueReplies(textgentest.FakeReply{
				StatusCode: testCase.statusCode, Header: http.Header{"Content-Type": {"application/json"}}, Body: testCase.body,
			})
			result := runMistralGenerateText(t, newMistralClient(t, provider.BaseURL(), "mistral-no-such-model"), mistralUserRequest("Hello"))
			require.Equal(t, llm.GenerateTextBranchProviderRejected, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.Equal(t, strings.TrimSpace(fmt.Sprintf("provider returned HTTP %d %s", testCase.statusCode,
				strings.Join(testCase.tokens, " "))), result.Failure.Message, "a Failure never carries provider message text")
			require.Equal(t, "mistral-no-such-model", result.Value.RequestedModel)
		})
	}
}

// TestMistralRateLimitErrorReturnsRetry keeps a documented 429 rate_limit_error retryable after Retry-After.
func TestMistralRateLimitErrorReturnsRetry(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, mistralDialect.CredentialHeader, mistralTestAPIKey)
	provider.EnqueueReplies(textgentest.FakeReply{
		StatusCode: http.StatusTooManyRequests, Header: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"7"}},
		Body: `{"object":"error","message":"Requests rate limit exceeded","type":"rate_limit_error","param":null,"code":null}`,
	})
	_, err := sdkgo.RunQuery(testsupport.NewDexContext("mistral-flow", "mistral-rate-limit"),
		newMistralClient(t, provider.BaseURL(), "").GenerateText(), mistralTestConnection, mistralUserRequest("Hello"))
	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError)
	require.Equal(t, sdkgo.FailureRateLimit, retryError.Failure.Kind)
	require.Equal(t, "provider returned HTTP 429 rate_limit_error", retryError.Failure.Message)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 7*time.Second, retryAfter.After)
}

func newMistralQuery(t testing.TB, connection textgentest.FakeConnection) *textgen.TextGenerationQuery {
	return newMistralFakeProviderClient(t, connection).GenerateText()
}

// newMistralFakeProviderClient builds a Client for a textgentest suite's fake provider and connection.
func newMistralFakeProviderClient(t testing.TB, connection textgentest.FakeConnection) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderMistral, Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[llm.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		llm.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newMistralClient(t *testing.T, baseURL string, model string) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderMistral, Model: model}, mistralTestCredentials(), llm.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func mistralTestCredentials() sdkgo.StaticCredentialProvider[llm.Credentials] {
	return sdkgo.StaticCredentialProvider[llm.Credentials]{mistralTestConnection: {APIKey: sdkgo.NewSecretString(mistralTestAPIKey)}}
}

func runMistralGenerateText(t *testing.T, client *llm.Client, request llm.GenerateTextRequest) llm.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("mistral-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("mistral-flow", stepExecutionID), client.GenerateText(), mistralTestConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), mistralTestAPIKey), "a Result contains the API key")
	return result
}

func mistralUserRequest(text string) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: text}}}
}
