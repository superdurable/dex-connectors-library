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
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
)

const xaiTestAPIKey = "xai-grok-connector-test-key-4f2a9d"

var xaiTestConnection = sdkgo.ConnectionRef{Provider: "llm", Name: "grok-test"}

// xaiDialect matches chatProfile and xAI's reasoning-token accounting.
var xaiDialect = newXAIProviderDialect("grok-4.3", "grok-4.7")

func TestXAIGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange := 2.1
	textgentest.RunTextGenerationExchangeSuite(t, &textgentest.TextGenerationExchangeSuite{
		Dialect:  xaiDialect,
		NewQuery: newXAIQuery,
		LocallyRejectedRequests: []textgentest.NamedTextGenerationRequest{
			{Name: "temperature above 2", Request: textgen.TextGenerationRequest{Temperature: &temperatureAboveRange}},
			{Name: "reasoning effort none on grok-4.7, which always reasons", Request: textgen.TextGenerationRequest{
				Model: "grok-4.7", ReasoningEffort: textgen.ReasoningEffortNone,
			}},
			{Name: "minimal reasoning effort, which xAI does not offer", Request: textgen.TextGenerationRequest{
				ReasoningEffort: textgen.ReasoningEffortMinimal,
			}},
			{Name: "max reasoning effort, which xAI does not offer", Request: textgen.TextGenerationRequest{
				Model: "grok-4.7", ReasoningEffort: textgen.ReasoningEffortMax,
			}},
			{Name: "xhigh reasoning effort on grok-4.5, which xAI serves as high", Request: textgen.TextGenerationRequest{
				Model: "grok-4.5", ReasoningEffort: textgen.ReasoningEffortExtraHigh,
			}},
			{Name: "reasoning effort on a Grok 4.20 alias", Request: textgen.TextGenerationRequest{
				Model: "grok-4.20-reasoning", ReasoningEffort: textgen.ReasoningEffortLow,
			}},
			{Name: "reasoning effort on grok-build-0.1", Request: textgen.TextGenerationRequest{
				Model: "grok-build-0.1", ReasoningEffort: textgen.ReasoningEffortHigh,
			}},
		},
	})
}

// TestXAIGenerateTextSendsTheDocumentedChatCompletionsRequest pins the body fields xAI documents for Chat Completions.
func TestXAIGenerateTextSendsTheDocumentedChatCompletionsRequest(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, xaiDialect.CredentialHeader, xaiTestAPIKey)
	// The dialect sends completion_tokens 9 beside reasoning_tokens 94, as xAI's API reference does.
	contractUsage := textgen.Usage{InputTokens: 32, CachedInputTokens: 6, OutputTokens: 103, ReasoningTokens: 94, TotalTokens: 135}
	provider.EnqueueReplies(xaiDialect.GeneratedReply(textgentest.GeneratedReply{
		Text: `{"answer":"TCP is reliable."}`, ServedModel: "grok-4.7", ResponseID: "0daf962f-a275-4a3c-839a-047854645532",
		Usage: contractUsage,
	}))
	temperature := 0.4
	result := runXAIGenerateText(t, newXAIClient(t, provider.BaseURL(), "grok-4.7"), llm.GenerateTextRequest{
		Instructions: "Answer in one sentence.",
		Messages:     []textgen.Message{{Role: textgen.MessageRoleUser, Text: "How does TCP differ from UDP?"}},
		StructuredOutput: &textgen.StructuredOutput{Name: "answer", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
			"required": []any{"answer"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature, ReasoningEffort: textgen.ReasoningEffortExtraHigh,
	})

	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, `{"answer":"TCP is reliable."}`, result.Value.Text)
	require.Equal(t, "grok-4.7", result.Value.RequestedModel)
	require.Equal(t, contractUsage, result.Value.Usage)
	require.Equal(t, "xai", result.Receipt.Provider)
	require.Equal(t, "0daf962f-a275-4a3c-839a-047854645532", result.Receipt.ProviderObjectID)
	requests := provider.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].Method)
	require.Equal(t, "/chat/completions", requests[0].Path, "the endpoint already ends in /v1")
	require.Equal(t, "text/event-stream", requests[0].Header.Get("Accept"))
	require.True(t, requests[0].HasCredentialInSlot, "the key travels as an Authorization bearer token")
	var body map[string]any
	require.NoError(t, json.Unmarshal(requests[0].Body, &body))
	require.Equal(t, map[string]any{
		"model": "grok-4.7",
		"messages": []any{
			map[string]any{"role": "system", "content": "Answer in one sentence."},
			map[string]any{"role": "user", "content": "How does TCP differ from UDP?"},
		},
		"max_completion_tokens": float64(2048),
		"temperature":           0.4,
		"reasoning_effort":      "xhigh",
		"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{
			"name": "answer", "strict": true, "schema": map[string]any{
				"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
				"required": []any{"answer"}, "additionalProperties": false,
			},
		}},
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}, body)
}

// TestXAIGenerateTextSendsEachModelsDocumentedReasoningEffort covers the per-model effort tables in profile.go.
func TestXAIGenerateTextSendsEachModelsDocumentedReasoningEffort(t *testing.T) {
	for _, testCase := range []struct {
		model     string
		effort    textgen.ReasoningEffort
		wireValue string
	}{
		{"grok-4.3", textgen.ReasoningEffortNone, "none"},
		{"grok-4.3-latest", textgen.ReasoningEffortExtraHigh, "xhigh"},
		{"grok-4.7", textgen.ReasoningEffortLow, "low"},
		{"grok-4.6", textgen.ReasoningEffortExtraHigh, "xhigh"},
		{"grok-4.5", textgen.ReasoningEffortHigh, "high"},
		{"grok-build-latest", textgen.ReasoningEffortMedium, "medium"},
	} {
		t.Run(testCase.model+"/"+string(testCase.effort), func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, xaiDialect.CredentialHeader, xaiTestAPIKey)
			provider.EnqueueReplies(xaiDialect.GeneratedReply(textgentest.GeneratedReply{Text: "Done.", ServedModel: testCase.model}))
			result := runXAIGenerateText(t, newXAIClient(t, provider.BaseURL(), testCase.model), llm.GenerateTextRequest{
				Messages:        []textgen.Message{{Role: textgen.MessageRoleUser, Text: "Plan the migration."}},
				ReasoningEffort: testCase.effort,
			})
			require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			var body struct {
				ReasoningEffort string `json:"reasoning_effort"`
			}
			require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
			require.Equal(t, testCase.wireValue, body.ReasoningEffort)
		})
	}
}

// TestXAIUsageCountsReasoningInOutputTokens uses xAI's API reference usage in a stream and a complete body.
func TestXAIUsageCountsReasoningInOutputTokens(t *testing.T) {
	const xaiUsage = `"usage":{"prompt_tokens":32,"completion_tokens":9,"total_tokens":135,` +
		`"prompt_tokens_details":{"text_tokens":32,"cached_tokens":6},"completion_tokens_details":{"reasoning_tokens":94},` +
		`"cost_in_usd_ticks":1234000}`
	for _, testCase := range []struct {
		name  string
		reply textgentest.FakeReply
	}{
		{"event stream", xaiStreamReply(
			`{"id":"chatcmpl-xai-1","object":"chat.completion.chunk","model":"grok-4.3","choices":[{"index":0,"delta":{"role":"assistant","content":"303"},"finish_reason":"stop"}],`+xaiUsage+`}`,
			"[DONE]",
		)},
		{"complete body from a gateway that ignored streaming", textgentest.FakeReply{
			Header: http.Header{"Content-Type": {"application/json"}},
			Body: `{"id":"chatcmpl-xai-1","object":"chat.completion","model":"grok-4.3",` +
				`"choices":[{"index":0,"message":{"role":"assistant","content":"303"},"finish_reason":"stop"}],` + xaiUsage + `}`,
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, xaiDialect.CredentialHeader, xaiTestAPIKey)
			provider.EnqueueReplies(testCase.reply)
			result := runXAIGenerateText(t, newXAIClient(t, provider.BaseURL(), ""), xaiUserRequest("What is 101*3?"))
			require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, "303", result.Value.Text)
			require.Equal(t, textgen.Usage{InputTokens: 32, CachedInputTokens: 6, OutputTokens: 103, ReasoningTokens: 94, TotalTokens: 135},
				result.Value.Usage, "OutputTokens includes the 94 reasoning tokens beside xAI's 9 completion tokens")
		})
	}
}

// TestXAIUsageCountsReasoningOnce adds reasoning to OutputTokens unless the total shows it is already there.
func TestXAIUsageCountsReasoningOnce(t *testing.T) {
	for _, testCase := range []struct {
		name, usage string
		expected    textgen.Usage
	}{
		{
			"completion tokens that already include reasoning, as OpenAI reports them",
			`{"prompt_tokens":32,"completion_tokens":103,"total_tokens":135,"completion_tokens_details":{"reasoning_tokens":94}}`,
			textgen.Usage{InputTokens: 32, OutputTokens: 103, ReasoningTokens: 94, TotalTokens: 135},
		},
		{
			"no reasoning tokens, as in xAI's deferred-completion example",
			`{"prompt_tokens":31,"completion_tokens":11,"total_tokens":42,"completion_tokens_details":{"reasoning_tokens":0}}`,
			textgen.Usage{InputTokens: 31, OutputTokens: 11, TotalTokens: 42},
		},
		{
			"a total with other billed tokens keeps xAI's accounting",
			`{"prompt_tokens":32,"completion_tokens":9,"total_tokens":200,"completion_tokens_details":{"reasoning_tokens":94}}`,
			textgen.Usage{InputTokens: 32, OutputTokens: 103, ReasoningTokens: 94, TotalTokens: 200},
		},
		{
			"no reported total keeps xAI's accounting",
			`{"prompt_tokens":32,"completion_tokens":9,"completion_tokens_details":{"reasoning_tokens":94}}`,
			textgen.Usage{InputTokens: 32, OutputTokens: 103, ReasoningTokens: 94, TotalTokens: 135},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, xaiDialect.CredentialHeader, xaiTestAPIKey)
			provider.EnqueueReplies(xaiStreamReply(
				`{"id":"chatcmpl-xai-4","object":"chat.completion.chunk","model":"grok-4.3","choices":[{"index":0,"delta":{"role":"assistant","content":"303"},"finish_reason":"stop"}],"usage":`+testCase.usage+`}`,
				"[DONE]",
			))
			result := runXAIGenerateText(t, newXAIClient(t, provider.BaseURL(), ""), xaiUserRequest("What is 101*3?"))
			require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, testCase.expected, result.Value.Usage)
		})
	}
}

// TestXAIEndTurnFinishSelectsGenerated accepts the end_turn finish token the API reference lists beside stop.
func TestXAIEndTurnFinishSelectsGenerated(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, xaiDialect.CredentialHeader, xaiTestAPIKey)
	provider.EnqueueReplies(xaiStreamReply(
		`{"id":"chatcmpl-xai-2","object":"chat.completion.chunk","model":"grok-4.3","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello."},"finish_reason":null}]}`,
		`{"id":"chatcmpl-xai-2","object":"chat.completion.chunk","model":"grok-4.3","choices":[{"index":0,"delta":{},"finish_reason":"end_turn"}]}`,
		"[DONE]",
	))
	result := runXAIGenerateText(t, newXAIClient(t, provider.BaseURL(), ""), xaiUserRequest("Hi"))
	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, textgen.FinishReasonStop, result.Value.FinishReason)
	require.Equal(t, "end_turn", result.Value.ProviderFinishReason)
	require.Equal(t, "Hello.", result.Value.Text)
}

// TestXAIErrorCodesReachTheFailureWithoutTheirMessage uses bodies the xAI API returned to a request without a valid key.
func TestXAIErrorCodesReachTheFailureWithoutTheirMessage(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		statusCode int
		body       string
		branch     sdkgo.BranchID
		kind       sdkgo.FailureKind
		errorCode  string
		message    string
	}{
		{
			"incorrect API key", http.StatusBadRequest,
			`{"code":"invalid-argument","error":"Incorrect API key provided. You can obtain an API key from https://console.x.ai."}`,
			llm.GenerateTextBranchProviderRejected, sdkgo.FailureProviderRejection, "invalid-argument", "Incorrect API key",
		},
		{
			"missing credentials", http.StatusUnauthorized,
			`{"code":"unauthenticated:no-credentials","error":"No credentials presented."}`,
			llm.GenerateTextBranchProviderRejected, sdkgo.FailureAuthentication, "unauthenticated:no-credentials", "No credentials",
		},
		{
			"model missing on the US endpoint", http.StatusNotFound,
			`{"code":"not-found","error":"The model grok-4.3 does not exist or your team team-1 does not have access to it."}`,
			llm.GenerateTextBranchProviderRejected, sdkgo.FailureNotFound, "not-found", "does not exist",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, xaiDialect.CredentialHeader, xaiTestAPIKey)
			provider.EnqueueReplies(textgentest.FakeReply{
				StatusCode: testCase.statusCode, Header: http.Header{"Content-Type": {"application/json"}}, Body: testCase.body,
			})
			result := runXAIGenerateText(t, newXAIClient(t, provider.BaseURL(), ""), xaiUserRequest("Hello"))
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, testCase.errorCode, "the /code token identifies the error")
			require.NotContains(t, result.Failure.Message, testCase.message, "a Failure never carries provider message text")
		})
	}
}

// TestXAIMidStreamErrorObjectSelectsInvalidResponse keeps an undocumented xAI stream error out of Retry and out of the Failure.
func TestXAIMidStreamErrorObjectSelectsInvalidResponse(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, xaiDialect.CredentialHeader, xaiTestAPIKey)
	provider.EnqueueReplies(xaiStreamReply(
		`{"id":"chatcmpl-xai-3","object":"chat.completion.chunk","model":"grok-4.3","choices":[{"index":0,"delta":{"role":"assistant","content":"Partial"},"finish_reason":null}]}`,
		`{"code":"internal","error":"Please retry your request."}`,
	))
	result := runXAIGenerateText(t, newXAIClient(t, provider.BaseURL(), ""), xaiUserRequest("Hello"))
	require.Equal(t, llm.GenerateTextBranchInvalidResponse, result.Branch)
	require.Empty(t, result.Value.Text)
	require.Contains(t, result.Failure.Message, "internal")
	require.NotContains(t, result.Failure.Message, "Please retry", "a Failure never carries provider message text")
}

func newXAIQuery(t testing.TB, connection textgentest.FakeConnection) *textgen.TextGenerationQuery {
	return newXAIFakeProviderClient(t, connection).GenerateText()
}

// newXAIFakeProviderClient builds a Client for a textgentest suite's fake provider and connection.
func newXAIFakeProviderClient(t testing.TB, connection textgentest.FakeConnection) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderXai, Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[llm.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		llm.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newXAIClient(t *testing.T, baseURL string, model string) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderXai, Model: model}, xaiTestCredentials(), llm.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func xaiTestCredentials() sdkgo.StaticCredentialProvider[llm.Credentials] {
	return sdkgo.StaticCredentialProvider[llm.Credentials]{xaiTestConnection: {APIKey: sdkgo.NewSecretString(xaiTestAPIKey)}}
}

func runXAIGenerateText(t *testing.T, client *llm.Client, request llm.GenerateTextRequest) llm.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("grok-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("grok-flow", stepExecutionID), client.GenerateText(), xaiTestConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), xaiTestAPIKey), "a Result contains the API key")
	return result
}

func xaiUserRequest(text string) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: text}}}
}

// xaiStreamReply renders each data payload as one server-sent event.
func xaiStreamReply(dataPayloads ...string) textgentest.FakeReply {
	var body strings.Builder
	for _, data := range dataPayloads {
		body.WriteString("data: " + data + "\n\n")
	}
	return textgentest.FakeReply{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body.String()}
}
