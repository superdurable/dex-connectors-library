// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mistral_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mistral"
	"github.com/superdurable/dex-connectors-library/connectors/mistral/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat/openaichattest"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "mistral-connector-test-key-0123456789abcdef"

var testConnection = sdkgo.ConnectionRef{Provider: "mistral", Name: "mistral-test"}

// mistralDialect matches chatProfile: streamed replies, the default 402 quota error, Mistral's top-level
// error envelope, and the mistral-correlation-id request ID.
var mistralDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "mistral-large-2512", AlternateModel: "mistral-small-2603", IsStreaming: true,
	RequestIDHeader: "mistral-correlation-id", ErrorTokenPointers: []string{"/type", "/code"},
})

func TestGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange := 1.6
	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
		Dialect:  mistralDialect,
		NewQuery: newMistralQuery,
		LocallyRejectedRequests: []llmtest.NamedTextGenerationRequest{
			{Name: "temperature above 1.5", Request: llm.TextGenerationRequest{Temperature: &temperatureAboveRange}},
			{Name: "reasoning effort on mistral-large-2512, which has no adjustable reasoning", Request: llm.TextGenerationRequest{
				ReasoningEffort: llm.ReasoningEffortHigh,
			}},
			{Name: "undocumented low effort on mistral-small-2603", Request: llm.TextGenerationRequest{
				Model: "mistral-small-2603", ReasoningEffort: llm.ReasoningEffortLow,
			}},
			{Name: "undocumented extra-high effort on mistral-medium-3-5", Request: llm.TextGenerationRequest{
				Model: "mistral-medium-3-5", ReasoningEffort: llm.ReasoningEffortExtraHigh,
			}},
			{Name: "reasoning effort on the deprecated mistral-medium-2508", Request: llm.TextGenerationRequest{
				Model: "mistral-medium-2508", ReasoningEffort: llm.ReasoningEffortNone,
			}},
			{Name: "reasoning effort on ministral-14b-2512", Request: llm.TextGenerationRequest{
				Model: "ministral-14b-2512", ReasoningEffort: llm.ReasoningEffortNone,
			}},
		},
	})
}

// TestGenerateTextSendsTheDocumentedChatCompletionsRequest pins the body fields Mistral's ChatCompletionRequest accepts.
func TestGenerateTextSendsTheDocumentedChatCompletionsRequest(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, mistralDialect.CredentialHeader, testAPIKey)
	reply := mistralDialect.GeneratedReply(llmtest.GeneratedReply{
		Text: `{"answer":"TCP is reliable."}`, ServedModel: "mistral-small-2603", ResponseID: "cmpl-mistral-1",
		Usage: llm.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, TotalTokens: 44},
	})
	reply.Header.Set("mistral-correlation-id", "01a0e51d-7e0c-708a-bddd-bcdc94fe7152")
	reply.Header.Set("x-ratelimit-remaining", "59")
	provider.EnqueueReplies(reply)
	temperature := 0.3
	result := runGenerateText(t, newMistralClient(t, provider.BaseURL(), ""), mistral.GenerateTextRequest{
		Model:        "mistral-small-2603",
		Instructions: "Answer in one sentence.",
		Messages:     []llm.Message{{Role: llm.MessageRoleUser, Text: "How does TCP differ from UDP?"}},
		StructuredOutput: &llm.StructuredOutput{Name: "answer", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
			"required": []any{"answer"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature, ReasoningEffort: llm.ReasoningEffortHigh,
	})

	require.Equal(t, mistral.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
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

// TestAdjustableReasoningModelsAcceptNoneAndHigh keeps each documented effort on each documented model ID and alias.
func TestAdjustableReasoningModelsAcceptNoneAndHigh(t *testing.T) {
	for _, model := range []string{"mistral-small-2603", "mistral-small-latest", "mistral-medium-3-5", "mistral-medium-3", "mistral-medium-latest"} {
		for effort, wireValue := range map[llm.ReasoningEffort]string{llm.ReasoningEffortNone: "none", llm.ReasoningEffortHigh: "high"} {
			t.Run(model+"/"+wireValue, func(t *testing.T) {
				provider := llmtest.NewFakeProvider(t, mistralDialect.CredentialHeader, testAPIKey)
				provider.EnqueueReplies(mistralDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Done.", ServedModel: model}))
				result := runGenerateText(t, newMistralClient(t, provider.BaseURL(), model), mistral.GenerateTextRequest{
					Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: "Plan the migration."}}, ReasoningEffort: effort,
				})
				require.Equal(t, mistral.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
				var body struct {
					ReasoningEffort string `json:"reasoning_effort"`
				}
				require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
				require.Equal(t, wireValue, body.ReasoningEffort)
			})
		}
	}
}

// TestReasoningStreamKeepsOnlyTheAnswerText replays Mistral's documented thinking-chunk stream shape.
func TestReasoningStreamKeepsOnlyTheAnswerText(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, mistralDialect.CredentialHeader, testAPIKey)
	chunk := func(delta string, finishReason string, usage string) string {
		finish := "null"
		if finishReason != "" {
			finish = `"` + finishReason + `"`
		}
		return fmt.Sprintf(`data: {"id":"cmpl-mistral-2","object":"chat.completion.chunk","created":1790000000,`+
			`"model":"mistral-medium-3-5","choices":[{"index":0,"delta":%s,"finish_reason":%s}],"usage":%s}`+"\n\n", delta, finish, usage)
	}
	provider.EnqueueReplies(llmtest.FakeReply{
		Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: chunk(`{"role":"assistant","content":[{"type":"thinking","thinking":[{"type":"text","text":"17 times 23 is 391."}]}]}`, "", "null") +
			chunk(`{"content":[{"type":"thinking","thinking":[]},{"type":"text","text":"17 * 23"}]}`, "", "null") +
			chunk(`{"content":" = 391."}`, "stop", `{"prompt_tokens":25,"completion_tokens":187,"total_tokens":212,"prompt_tokens_details":{"cached_tokens":0}}`) +
			"data: [DONE]\n\n",
	})
	result := runGenerateText(t, newMistralClient(t, provider.BaseURL(), "mistral-medium-3-5"), mistral.GenerateTextRequest{
		Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: "What is 17 * 23?"}}, ReasoningEffort: llm.ReasoningEffortHigh,
	})
	require.Equal(t, mistral.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "17 * 23 = 391.", result.Value.Text, "the thinking trace never reaches the text")
	require.Equal(t, "mistral-medium-3-5", result.Value.ServedModel)
	require.Equal(t, llm.Usage{InputTokens: 25, OutputTokens: 187, TotalTokens: 212}, result.Value.Usage)
}

// TestStreamUsageArrivesWithTheFinishChunk replays the documented stream, whose last chunk has both finish_reason and usage.
func TestStreamUsageArrivesWithTheFinishChunk(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, mistralDialect.CredentialHeader, testAPIKey)
	chunk := func(delta string, finishReason string, usage string) string {
		return `data: {"id":"59060ef9339a4112b2c9e57e3ee6199d","model":"mistral-large-latest","choices":[{"index":0,"delta":` + delta +
			`,"finish_reason":` + finishReason + `}],"object":"chat.completion.chunk","created":1764258570,"usage":` + usage + "}\n\n"
	}
	provider.EnqueueReplies(llmtest.FakeReply{
		Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: chunk(`{"role":"assistant","content":""}`, "null", "null") + chunk(`{"content":"3"}`, "null", "null") +
			chunk(`{"content":"84"}`, "null", "null") + chunk(`{"content":"4"}`, "null", "null") +
			chunk(`{"content":"00"}`, `"stop"`, `{"prompt_tokens":19,"completion_tokens":7,"total_tokens":26}`) + "data: [DONE]\n\n",
	})
	result := runGenerateText(t, newMistralClient(t, provider.BaseURL(), "mistral-large-latest"), userRequest("How far is the moon?"))
	require.Equal(t, mistral.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "384400", result.Value.Text)
	require.Equal(t, "59060ef9339a4112b2c9e57e3ee6199d", result.Value.ResponseID)
	require.Equal(t, llm.Usage{InputTokens: 19, OutputTokens: 7, TotalTokens: 26}, result.Value.Usage)
}

// TestFinishReasonsFollowMistralsEnum maps model_length to truncated and keeps error and tool_calls invalid responses.
func TestFinishReasonsFollowMistralsEnum(t *testing.T) {
	for _, testCase := range []struct {
		finishReason string
		branch       sdkgo.BranchID
		text         string
	}{
		{"model_length", mistral.GenerateTextBranchTruncated, "Partial answer"},
		{"length", mistral.GenerateTextBranchTruncated, "Partial answer"},
		{"error", mistral.GenerateTextBranchInvalidResponse, ""},
		{"tool_calls", mistral.GenerateTextBranchInvalidResponse, ""},
	} {
		t.Run(testCase.finishReason, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, mistralDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(llmtest.FakeReply{
				Header: http.Header{"Content-Type": {"text/event-stream"}},
				Body: `data: {"id":"cmpl-mistral-3","object":"chat.completion.chunk","model":"mistral-large-2512",` +
					`"choices":[{"index":0,"delta":{"role":"assistant","content":"Partial answer"},"finish_reason":"` + testCase.finishReason + `"}],` +
					`"usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}}` + "\n\ndata: [DONE]\n\n",
			})
			result := runGenerateText(t, newMistralClient(t, provider.BaseURL(), ""), userRequest("Hello"))
			require.Equal(t, testCase.branch, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, testCase.text, result.Value.Text)
			require.Equal(t, testCase.finishReason, result.Value.ProviderFinishReason)
		})
	}
}

// TestErrorEnvelopeTokensReachTheFailure reads the documented top-level type and code, never the message.
func TestErrorEnvelopeTokensReachTheFailure(t *testing.T) {
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
			provider := llmtest.NewFakeProvider(t, mistralDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(llmtest.FakeReply{
				StatusCode: testCase.statusCode, Header: http.Header{"Content-Type": {"application/json"}}, Body: testCase.body,
			})
			result := runGenerateText(t, newMistralClient(t, provider.BaseURL(), "mistral-no-such-model"), userRequest("Hello"))
			require.Equal(t, mistral.GenerateTextBranchProviderRejected, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.Equal(t, strings.TrimSpace(fmt.Sprintf("provider returned HTTP %d %s", testCase.statusCode,
				strings.Join(testCase.tokens, " "))), result.Failure.Message, "a Failure never carries provider message text")
			require.Equal(t, "mistral-no-such-model", result.Value.RequestedModel)
		})
	}
}

// TestRateLimitErrorReturnsRetry keeps a documented 429 rate_limit_error retryable after Retry-After.
func TestRateLimitErrorReturnsRetry(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, mistralDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(llmtest.FakeReply{
		StatusCode: http.StatusTooManyRequests, Header: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"7"}},
		Body: `{"object":"error","message":"Requests rate limit exceeded","type":"rate_limit_error","param":null,"code":null}`,
	})
	_, err := sdkgo.RunQuery(testsupport.NewDexContext("mistral-flow", "mistral-rate-limit"),
		newMistralClient(t, provider.BaseURL(), "").GenerateText(), testConnection, userRequest("Hello"))
	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError)
	require.Equal(t, sdkgo.FailureRateLimit, retryError.Failure.Kind)
	require.Equal(t, "provider returned HTTP 429 rate_limit_error", retryError.Failure.Message)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 7*time.Second, retryAfter.After)
}

// TestGenerateTextCallsTheConfiguredMistralEndpoint proves each documented production URL without a network call.
func TestGenerateTextCallsTheConfiguredMistralEndpoint(t *testing.T) {
	for _, testCase := range []struct{ endpoint, requestedURL string }{
		{"", "https://api.mistral.ai/v1/chat/completions"},
		{mistral.GlobalEndpoint, "https://api.mistral.ai/v1/chat/completions"},
		{mistral.EUEndpoint, "https://api.eu.mistral.ai/v1/chat/completions"},
		{mistral.USEndpoint + "/", "https://api.us.mistral.ai/v1/chat/completions"},
		{" " + mistral.EUEndpoint + " ", "https://api.eu.mistral.ai/v1/chat/completions"},
	} {
		t.Run(testCase.requestedURL, func(t *testing.T) {
			reply := mistralDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Hello.", ServedModel: "mistral-large-2512"})
			var requestedURLs []string
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requestedURLs = append(requestedURLs, request.URL.String())
				return &http.Response{
					StatusCode: http.StatusOK, Header: reply.Header.Clone(), Body: io.NopCloser(strings.NewReader(reply.Body)), Request: request,
				}, nil
			})
			client, err := mistral.New(mistral.Config{Model: " ", Endpoint: testCase.endpoint}, testCredentials(),
				mistral.WithHTTPClient(&http.Client{Transport: transport}))
			require.NoError(t, err)
			result := runGenerateText(t, client, userRequest("Hello"))
			require.Equal(t, mistral.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, "mistral-large-2512", result.Value.RequestedModel, "a blank connection model uses the manifest default")
			require.Equal(t, []string{testCase.requestedURL}, requestedURLs)
		})
	}
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	for _, endpoint := range []string{
		"https://api.mistral.ai", "https://api.mistral.ai/v2", "http://api.mistral.ai/v1", "https://api.mistral.ai.example.com/v1",
		"https://codestral.mistral.ai/v1", "https://attacker.example/v1", "http://127.0.0.1:8080", "https://api.mistral.ai/v1?x=1",
		"https://api.mistral.ai/v1//",
	} {
		_, err := mistral.New(mistral.Config{Endpoint: endpoint}, testCredentials())
		require.Error(t, err, endpoint)
		require.NotContains(t, err.Error(), testAPIKey)
	}
	_, err := mistral.New(mistral.Config{Endpoint: "https://attacker.example/v1"}, testCredentials())
	require.EqualError(t, err, "mistral endpoint must be one of https://api.mistral.ai/v1, https://api.eu.mistral.ai/v1, https://api.us.mistral.ai/v1")
	for _, baseURL := range []string{"https://api.mistral.ai/v1", "http://192.0.2.10:8080", "https://attacker.example"} {
		_, err := mistral.New(mistral.Config{}, testCredentials(), mistral.WithBaseURLForTest(baseURL))
		require.ErrorContains(t, err, "loopback", baseURL)
	}
	for _, baseURL := range []string{"http://127.0.0.1:8080", "http://localhost:9000", "http://[::1]:7000"} {
		_, err := mistral.New(mistral.Config{}, testCredentials(), mistral.WithBaseURLForTest(baseURL))
		require.NoError(t, err, baseURL)
	}
	_, err = mistral.New(mistral.Config{Endpoint: "https://attacker.example/v1"}, testCredentials(), mistral.WithBaseURLForTest("http://127.0.0.1:8080"))
	require.ErrorContains(t, err, "endpoint must be one of", "the test base URL does not bypass endpoint validation")
	_, err = mistral.New(mistral.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = mistral.New(mistral.Config{Model: "mistral-é"}, testCredentials())
	require.Error(t, err, "an invalid connection model fails at startup")
	_, err = mistral.New(mistral.Config{MaxResponseBytes: -1}, testCredentials())
	require.ErrorContains(t, err, "maxResponseBytes")
	_, err = mistral.New(mistral.Config{}, testCredentials(), nil)
	require.ErrorContains(t, err, "option is nil")
}

func newMistralQuery(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
	return newFakeProviderClient(t, connection).GenerateText()
}

// newFakeProviderClient builds a Client for an llmtest suite's fake provider and connection.
func newFakeProviderClient(t testing.TB, connection llmtest.FakeConnection) *mistral.Client {
	t.Helper()
	client, err := mistral.New(mistral.Config{Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[mistral.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		mistral.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newMistralClient(t *testing.T, baseURL string, model string) *mistral.Client {
	t.Helper()
	client, err := mistral.New(mistral.Config{Model: model}, testCredentials(), mistral.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func testCredentials() sdkgo.StaticCredentialProvider[mistral.Credentials] {
	return sdkgo.StaticCredentialProvider[mistral.Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
}

func runGenerateText(t *testing.T, client *mistral.Client, request mistral.GenerateTextRequest) mistral.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("mistral-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("mistral-flow", stepExecutionID), client.GenerateText(), testConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), testAPIKey), "a Result contains the API key")
	return result
}

func userRequest(text string) mistral.GenerateTextRequest {
	return mistral.GenerateTextRequest{Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: text}}}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
