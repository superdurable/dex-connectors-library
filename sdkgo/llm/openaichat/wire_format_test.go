// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openaichat_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat/openaichattest"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	chatAPIKey = "sk-chat-test-key"
	chatModel  = "chat-model"
)

var (
	chatConnection = sdkgo.ConnectionRef{Provider: "chat", Name: "test"}
	chatDefinition = sdkgo.QueryDefinition{
		Operation:    sdkgo.OperationRef{ConnectorID: "chat-lab", OperationID: llm.TextGenerationOperationID},
		Branches:     llm.TextGenerationBranchDefinitions(),
		StepDefaults: sdkgo.StepDefaults{ExecuteMethodTimeout: time.Minute, ExecuteDurability: dex.StepDurabilitySync},
	}
	bearerHeader = llm.CredentialHeader{Name: "Authorization", Prefix: "Bearer "}
)

func TestChatWireFormatFollowsTheExchangeContract(t *testing.T) {
	profile := &openaichat.Profile{
		ProviderName:     "chat-lab",
		StructuredOutput: llm.StructuredOutputRules{Mode: llm.StructuredOutputModeJSONObjectWithInstruction},
		ReasoningEfforts: map[llm.ReasoningEffort]string{llm.ReasoningEffortLow: "low"},
	}
	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
		Dialect: openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
			ConnectionModel: chatModel, AlternateModel: "chat-model-2",
		}),
		NewQuery: func(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
			return newChatQueryFor(t, profile, connection.BaseURL, connection.Model, connection.MaxResponseBytes,
				func(call sdkgo.Call) (sdkgo.SecretString, error) {
					return sdkgo.StaticCredentialProvider[sdkgo.SecretString]{connection.Reference: connection.APIKey}.Resolve(call)
				})
		},
		LocallyRejectedRequests: []llmtest.NamedTextGenerationRequest{
			{Name: "unmapped reasoning effort", Request: llm.TextGenerationRequest{ReasoningEffort: llm.ReasoningEffortHigh}},
		},
	})
}

// TestChatWireFormatWithATopLevelErrorEnvelope models a strict provider whose errors are {"type", "code", "message"}.
func TestChatWireFormatWithATopLevelErrorEnvelope(t *testing.T) {
	profile := &openaichat.Profile{
		ProviderName:               "strict-lab",
		StructuredOutput:           llm.StructuredOutputRules{Mode: llm.StructuredOutputModeJSONSchema},
		ShouldSendStrictJSONSchema: true,
		AllowedRequestFields:       []string{"model", "messages", "max_tokens", "temperature", "response_format"},
		ErrorTokenPointers:         []string{"/type", "/code"},
		ErrorRules: []llm.ErrorRule{
			{StatusCode: http.StatusTooManyRequests, ErrorToken: "lab_quota_exceeded", Outcome: llm.QuotaExhaustedOutcome()},
			{StatusCode: http.StatusBadRequest, ErrorToken: "lab_content_blocked", Outcome: llm.BlockedOutcome()},
		},
	}
	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
		Dialect: openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
			ConnectionModel: chatModel, AlternateModel: "chat-model-2", ErrorTokenPointers: []string{"/type", "/code"},
			QuotaExhaustedStatusCode: http.StatusTooManyRequests, QuotaExhaustedErrorToken: "lab_quota_exceeded",
			ContentPolicyErrorToken: "lab_content_blocked",
		}),
		NewQuery: func(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
			return newChatQueryFor(t, profile, connection.BaseURL, connection.Model, connection.MaxResponseBytes,
				func(call sdkgo.Call) (sdkgo.SecretString, error) {
					return sdkgo.StaticCredentialProvider[sdkgo.SecretString]{connection.Reference: connection.APIKey}.Resolve(call)
				})
		},
	})
}

func TestChatRequestBodies(t *testing.T) {
	temperature := 0.7
	strictSchema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []any{"n"},
		"properties": map[string]any{"n": map[string]any{"type": "integer", "minimum": 1}},
	}
	userMessage := `{"role":"user","content":"hi"}`
	cases := []struct {
		name     string
		profile  openaichat.Profile
		request  llm.TextGenerationRequest
		expected string
	}{
		{
			name:     "defaults send no sampling fields",
			request:  chatRequest(nil),
			expected: `{"model":"chat-model","messages":[` + userMessage + `]}`,
		},
		{
			name: "system instructions, max_tokens, temperature, and effort",
			profile: openaichat.Profile{ReasoningEfforts: map[llm.ReasoningEffort]string{
				llm.ReasoningEffortExtraHigh: "very_high",
			}},
			request: chatRequest(func(request *llm.TextGenerationRequest) {
				request.Instructions, request.MaxOutputTokens, request.Temperature = "Be brief.", 64, &temperature
				request.ReasoningEffort = llm.ReasoningEffortExtraHigh
			}),
			expected: `{"model":"chat-model","messages":[{"role":"system","content":"Be brief."},` + userMessage + `],
				"max_tokens":64,"temperature":0.7,"reasoning_effort":"very_high"}`,
		},
		{
			name:    "developer instructions and max_completion_tokens",
			profile: openaichat.Profile{InstructionsRole: openaichat.InstructionsRoleDeveloper, MaxTokensField: openaichat.MaxTokensFieldMaxCompletionTokens},
			request: chatRequest(func(request *llm.TextGenerationRequest) {
				request.Instructions, request.MaxOutputTokens = "Be brief.", 64
			}),
			expected: `{"model":"chat-model","messages":[{"role":"developer","content":"Be brief."},` + userMessage + `],"max_completion_tokens":64}`,
		},
		{
			name: "strict json_schema with transforms",
			profile: openaichat.Profile{ShouldSendStrictJSONSchema: true, StructuredOutput: llm.StructuredOutputRules{
				Mode: llm.StructuredOutputModeJSONSchema, KeywordsMovedToDescription: []string{"minimum"},
			}},
			request: chatRequest(func(request *llm.TextGenerationRequest) {
				request.StructuredOutput = &llm.StructuredOutput{Name: "count", Description: "A count.", Schema: strictSchema}
			}),
			expected: `{"model":"chat-model","messages":[` + userMessage + `],"response_format":{"type":"json_schema","json_schema":{
				"name":"count","description":"A count.","strict":true,"schema":{"type":"object","additionalProperties":false,
				"required":["n"],"properties":{"n":{"type":"integer","description":"(minimum: 1)"}}}}}}`,
		},
		{
			name:    "streaming with usage",
			profile: openaichat.Profile{Streaming: openaichat.StreamingPolicyAlways, ShouldRequestStreamUsage: true},
			request: chatRequest(nil),
			expected: `{"model":"chat-model","messages":[` + userMessage + `],"stream":true,
				"stream_options":{"include_usage":true}}`,
		},
		{
			name: "the first matching model rule applies",
			profile: openaichat.Profile{ModelRules: []openaichat.ModelRule{
				{ModelIDPattern: `^chat-(model|other)$`, MaxTokensField: openaichat.MaxTokensFieldMaxCompletionTokens, Streaming: openaichat.StreamingPolicyAlways},
				{ModelIDPrefix: "chat-", MaxTokensField: openaichat.MaxTokensFieldMaxTokens},
			}},
			request: chatRequest(func(request *llm.TextGenerationRequest) { request.MaxOutputTokens = 8 }),
			expected: `{"model":"chat-model","messages":[` + userMessage + `],"max_completion_tokens":8,
				"stream":true}`,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			profile := testCase.profile
			profile.ProviderName = "chat-lab"
			provider := llmtest.NewFakeProvider(t, bearerHeader, chatAPIKey)
			query := newChatQuery(t, &profile, provider)
			isStreaming := profile.Streaming == openaichat.StreamingPolicyAlways || len(profile.ModelRules) > 0
			dialect := openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
				ConnectionModel: chatModel, AlternateModel: "unused", IsStreaming: isStreaming,
			})
			text := "ok"
			if testCase.request.StructuredOutput != nil {
				text = `{"n":2}`
			}
			provider.EnqueueReplies(dialect.GeneratedReply(llmtest.GeneratedReply{Text: text, ServedModel: chatModel, ResponseID: "r"}))
			result, err := runChatQuery(query, testCase.request)
			require.NoError(t, err)
			require.Equal(t, llm.GeneratedBranchID, result.Branch, "failure: %+v", result.Failure)
			requests := provider.Requests()
			require.Len(t, requests, 1)
			require.Equal(t, "/v1/chat/completions", requests[0].Path)
			require.JSONEq(t, testCase.expected, string(requests[0].Body))
		})
	}
}

func TestChatJSONObjectModeAddsTheInstruction(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, bearerHeader, chatAPIKey)
	query := newChatQuery(t, &openaichat.Profile{
		ProviderName: "chat-lab", StructuredOutput: llm.StructuredOutputRules{Mode: llm.StructuredOutputModeJSONObjectWithInstruction},
	}, provider)
	dialect := openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{ConnectionModel: chatModel, AlternateModel: "unused"})
	provider.EnqueueReplies(dialect.GeneratedReply(llmtest.GeneratedReply{Text: `{"ok":true}`}))
	result, err := runChatQuery(query, chatRequest(func(request *llm.TextGenerationRequest) {
		request.StructuredOutput = &llm.StructuredOutput{Name: "verdict", Schema: map[string]any{
			"type": "object", "additionalProperties": false, "required": []any{"ok"},
			"properties": map[string]any{"ok": map[string]any{"type": "boolean"}},
		}}
	}))
	require.NoError(t, err)
	require.Equal(t, llm.GeneratedBranchID, result.Branch)
	body := string(provider.Requests()[0].Body)
	require.Contains(t, body, `"response_format":{"type":"json_object"}`)
	require.Contains(t, body, `{"role":"system","content":"Respond with only one JSON object`)
	require.Contains(t, body, `named \"verdict\"`)
}

func TestChatAllowlistRejectsUnacceptedFieldsWithoutARequest(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, bearerHeader, chatAPIKey)
	query := newChatQuery(t, &openaichat.Profile{
		ProviderName: "chat-lab", Streaming: openaichat.StreamingPolicyAlways, ShouldRequestStreamUsage: true,
		AllowedRequestFields: []string{"model", "messages", "max_tokens", "stream", "stream_options"},
	}, provider)
	temperature := 0.5
	result, err := runChatQuery(query, chatRequest(func(request *llm.TextGenerationRequest) { request.Temperature = &temperature }))
	require.NoError(t, err)
	require.Equal(t, llm.DefectBranchID, result.Branch)
	require.Contains(t, result.Failure.Message, `"temperature"`)
	require.Empty(t, provider.Requests())
}

func TestChatHeadersAndReceipt(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, llm.CredentialHeader{Name: "x-api-key"}, chatAPIKey)
	query := newChatQuery(t, &openaichat.Profile{
		ProviderName: "chat-lab", CredentialHeader: llm.CredentialHeader{Name: "x-api-key"},
		FixedHeaders:     map[string]string{"Chat-Version": "2026-09-01"},
		RequestIDHeaders: []string{"request-id"}, RateLimitHeaders: []string{"x-ratelimit-remaining-tokens"},
	}, provider)
	dialect := openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{ConnectionModel: chatModel, AlternateModel: "unused"})
	reply := dialect.GeneratedReply(llmtest.GeneratedReply{Text: "ok", ResponseID: "chatcmpl-1"})
	reply.Header.Set("request-id", "req-9")
	reply.Header.Set("x-ratelimit-remaining-tokens", "900")
	provider.EnqueueReplies(reply)
	result, err := runChatQuery(query, chatRequest(nil))
	require.NoError(t, err)
	require.Equal(t, "req-9", result.Receipt.ProviderRequestID)
	require.Equal(t, "chatcmpl-1", result.Receipt.ProviderObjectID)
	require.Equal(t, map[string]string{"x-ratelimit-remaining-tokens": "900"}, result.Receipt.Metadata)
	recorded := provider.Requests()[0]
	require.True(t, recorded.HasCredentialInSlot)
	require.Empty(t, recorded.Header.Get("Authorization"))
	require.Equal(t, "2026-09-01", recorded.Header.Get("Chat-Version"))
}

func TestChatDecodesProviderVariants(t *testing.T) {
	jsonReply := func(statusCode int, body string) llmtest.FakeReply {
		return llmtest.FakeReply{StatusCode: statusCode, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}
	}
	streamReply := func(body string) llmtest.FakeReply {
		return llmtest.FakeReply{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}
	}
	completion := func(message string, finishReason string) string {
		return `{"id":"c1","model":"served","choices":[{"index":0,"message":` + message + `,"finish_reason":` + finishReason + `}],
			"usage":{"prompt_tokens":12.0,"completion_tokens":3,"total_tokens":null}}`
	}
	chunk := func(fields string) string { return "data: {\"id\":\"c1\",\"model\":\"served\"," + fields + "}\n\n" }
	cases := []struct {
		name      string
		profile   openaichat.Profile
		reply     llmtest.FakeReply
		branch    sdkgo.BranchID
		retryKind sdkgo.FailureKind
		text      string
		finish    llm.FinishReason
		usage     llm.Usage
	}{
		{
			name:   "content parts skip thinking",
			reply:  jsonReply(200, completion(`{"content":[{"type":"thinking","thinking":"hmm"},{"type":"text","text":"A"},{"type":"text","text":"B"}]}`, `"stop"`)),
			branch: llm.GeneratedBranchID, text: "AB", finish: llm.FinishReasonStop,
			usage: llm.Usage{InputTokens: 12, OutputTokens: 3, TotalTokens: 15},
		},
		{
			name:   "refusal is blocked",
			reply:  jsonReply(200, completion(`{"content":null,"refusal":"I cannot help with that."}`, `"stop"`)),
			branch: llm.BlockedBranchID, finish: llm.FinishReasonRefusal,
		},
		{
			name:    "extended finish token",
			profile: openaichat.Profile{FinishReasons: map[string]llm.FinishReason{"model_length": llm.FinishReasonLength}},
			reply:   jsonReply(200, completion(`{"content":"par"}`, `"model_length"`)),
			branch:  llm.TruncatedBranchID, text: "par", finish: llm.FinishReasonLength,
		},
		{
			name:   "tool call finish is unusable",
			reply:  jsonReply(200, completion(`{"content":null}`, `"tool_calls"`)),
			branch: llm.InvalidResponseBranchID,
		},
		{
			name:   "fractional token count",
			reply:  jsonReply(200, `{"choices":[{"index":0,"message":{"content":"x"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1.5}}`),
			branch: llm.InvalidResponseBranchID,
		},
		{
			name:   "no first choice",
			reply:  jsonReply(200, `{"choices":[{"index":1,"message":{"content":"x"},"finish_reason":"stop"}]}`),
			branch: llm.InvalidResponseBranchID,
		},
		{
			name:   "error body on 200",
			reply:  jsonReply(200, `{"error":{"type":"server_busy","message":"secret provider text"}}`),
			branch: llm.InvalidResponseBranchID,
		},
		{
			name: "error body on 200 matched by a status-independent rule",
			profile: openaichat.Profile{ErrorRules: []llm.ErrorRule{
				{ErrorToken: "server_busy", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
			}},
			reply:     jsonReply(200, `{"error":{"type":"server_busy","message":"secret provider text"}}`),
			retryKind: sdkgo.FailureAvailability,
		},
		{
			name:    "stream with reasoning deltas and final usage chunk",
			profile: openaichat.Profile{Streaming: openaichat.StreamingPolicyAlways},
			reply: streamReply(chunk(`"choices":[{"index":0,"delta":{"reasoning_content":"think"}}]`) +
				chunk(`"choices":[{"index":0,"delta":{"content":"Hi"}}]`) +
				chunk(`"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]`) +
				chunk(`"choices":[],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3,"completion_tokens_details":{"reasoning_tokens":1}}`)),
			branch: llm.GeneratedBranchID, text: "Hi", finish: llm.FinishReasonStop,
			usage: llm.Usage{InputTokens: 2, OutputTokens: 1, ReasoningTokens: 1, TotalTokens: 3},
		},
		{
			name:      "stream ends before a finish",
			profile:   openaichat.Profile{Streaming: openaichat.StreamingPolicyAlways},
			reply:     streamReply(chunk(`"choices":[{"index":0,"delta":{"content":"Hi"}}]`)),
			retryKind: sdkgo.FailureTransport,
		},
		{
			name:    "stream sends DONE without a finish",
			profile: openaichat.Profile{Streaming: openaichat.StreamingPolicyAlways},
			reply:   streamReply(chunk(`"choices":[{"index":0,"delta":{"content":"Hi"}}]`) + "data: [DONE]\n\n"),
			branch:  llm.InvalidResponseBranchID,
		},
		{
			name: "stream error event",
			profile: openaichat.Profile{Streaming: openaichat.StreamingPolicyAlways, ErrorRules: []llm.ErrorRule{
				{ErrorToken: "overloaded_error", Outcome: llm.RetryOutcome(sdkgo.FailureAvailability)},
			}},
			reply: streamReply(chunk(`"choices":[{"index":0,"delta":{"content":"Hi"}}]`) +
				"event: error\ndata: {\"error\":{\"type\":\"overloaded_error\",\"message\":\"secret provider text\"}}\n\n"),
			retryKind: sdkgo.FailureAvailability,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			profile := testCase.profile
			profile.ProviderName = "chat-lab"
			provider := llmtest.NewFakeProvider(t, bearerHeader, chatAPIKey)
			provider.EnqueueReplies(testCase.reply)
			result, err := runChatQuery(newChatQuery(t, &profile, provider), chatRequest(nil))
			if testCase.retryKind != "" {
				var retryError *sdkgo.RetryError
				require.ErrorAs(t, err, &retryError)
				require.Equal(t, testCase.retryKind, retryError.Failure.Kind)
				require.NotContains(t, retryError.Error(), "secret provider text")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, testCase.text, result.Value.Text)
			if testCase.finish != "" {
				require.Equal(t, testCase.finish, result.Value.FinishReason)
			}
			if testCase.usage != (llm.Usage{}) {
				require.Equal(t, testCase.usage, result.Value.Usage)
			}
			if result.Failure != nil {
				require.NotContains(t, result.Failure.Message, "secret provider text")
			}
		})
	}
}

func TestNewWireFormatRejectsInvalidProfiles(t *testing.T) {
	cases := map[string]openaichat.Profile{
		"missing provider name":      {},
		"relative path":              {ProviderName: "p", ChatCompletionsPath: "chat/completions"},
		"path with a query":          {ProviderName: "p", ChatCompletionsPath: "/chat?x=1"},
		"reserved fixed header":      {ProviderName: "p", FixedHeaders: map[string]string{"authorization": "Bearer x"}},
		"fixed header value":         {ProviderName: "p", FixedHeaders: map[string]string{"X-Version": "1\n2"}},
		"instructions role":          {ProviderName: "p", InstructionsRole: "tool"},
		"token field":                {ProviderName: "p", MaxTokensField: "max_output_tokens"},
		"streaming policy":           {ProviderName: "p", Streaming: "sometimes"},
		"effort wire value":          {ProviderName: "p", ReasoningEfforts: map[llm.ReasoningEffort]string{llm.ReasoningEffortLow: "Low Effort"}},
		"unknown effort":             {ProviderName: "p", ReasoningEfforts: map[llm.ReasoningEffort]string{"ultra": "ultra"}},
		"structured mode":            {ProviderName: "p", StructuredOutput: llm.StructuredOutputRules{Mode: "xml"}},
		"request field":              {ProviderName: "p", AllowedRequestFields: []string{"Model"}},
		"allowlist without messages": {ProviderName: "p", AllowedRequestFields: []string{"model", "max_tokens"}},
		"allowlist without stream": {ProviderName: "p", AllowedRequestFields: []string{"model", "messages"},
			ModelRules: []openaichat.ModelRule{{ModelIDPrefix: "streamer", Streaming: openaichat.StreamingPolicyAlways}}},
		"allowlist without stream options": {ProviderName: "p", Streaming: openaichat.StreamingPolicyAlways,
			ShouldRequestStreamUsage: true, AllowedRequestFields: []string{"model", "messages", "stream"}},
		"rule with both matchers": {ProviderName: "p", ModelRules: []openaichat.ModelRule{
			{ModelIDPrefix: "a", ModelIDPattern: "^a$"},
		}},
		"rule without a matcher":   {ProviderName: "p", ModelRules: []openaichat.ModelRule{{}}},
		"unanchored rule pattern":  {ProviderName: "p", ModelRules: []openaichat.ModelRule{{ModelIDPattern: "gpt"}}},
		"invalid rule pattern":     {ProviderName: "p", ModelRules: []openaichat.ModelRule{{ModelIDPattern: "^(a$"}}},
		"invalid rule token field": {ProviderName: "p", ModelRules: []openaichat.ModelRule{{ModelIDPrefix: "a", MaxTokensField: "tokens"}}},
	}
	for name, profile := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := openaichat.NewWireFormat(&profile)
			require.Error(t, err)
		})
	}
	_, err := openaichat.NewWireFormat(nil)
	require.Error(t, err)
}

func chatRequest(customize func(*llm.TextGenerationRequest)) llm.TextGenerationRequest {
	request := llm.TextGenerationRequest{Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: "hi"}}}
	if customize != nil {
		customize(&request)
	}
	return request
}

func newChatQuery(t testing.TB, profile *openaichat.Profile, provider *llmtest.FakeProvider) *llm.TextGenerationQuery {
	t.Helper()
	return newChatQueryFor(t, profile, provider.BaseURL()+"/v1", chatModel, 1<<16,
		func(sdkgo.Call) (sdkgo.SecretString, error) { return sdkgo.NewSecretString(chatAPIKey), nil })
}

func newChatQueryFor(
	t testing.TB, profile *openaichat.Profile, baseURL string, model string, maxResponseBytes int64,
	resolveCredential func(sdkgo.Call) (sdkgo.SecretString, error),
) *llm.TextGenerationQuery {
	t.Helper()
	wireFormat, err := openaichat.NewWireFormat(profile)
	require.NoError(t, err)
	query, err := llm.NewTextGenerationQuery(&llm.TextGenerationQueryConfig{
		Definition: chatDefinition, WireFormat: wireFormat, BaseURL: baseURL, ConnectionModel: model,
		RequestTimeout: 10 * time.Second, ResolveCredential: resolveCredential,
		MaxResponseBytes: maxResponseBytes, MaxStreamEventBytes: 1 << 14,
	})
	require.NoError(t, err)
	return query
}

func runChatQuery(query *llm.TextGenerationQuery, request llm.TextGenerationRequest) (sdkgo.QueryResult[llm.TextGenerationResponse], error) {
	ctx := testsupport.NewDexContext("chat-flow", fmt.Sprintf("step-%d", time.Now().UnixNano()))
	return sdkgo.RunQuery(ctx, query, chatConnection, request)
}
