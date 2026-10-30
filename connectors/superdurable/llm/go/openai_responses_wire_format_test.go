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

const openAITestAPIKey = "sk-proj-openai-connector-generate-text-key"

var openAITestConnection = sdkgo.ConnectionRef{Provider: "llm", Name: "openai-generate-text"}

func TestOpenAIGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange, temperature := 2.1, 0.4
	textgentest.RunTextGenerationExchangeSuite(t, &textgentest.TextGenerationExchangeSuite{
		Dialect:  responsesDialect,
		NewQuery: newOpenAIQuery,
		LocallyRejectedRequests: []textgentest.NamedTextGenerationRequest{
			{Name: "temperature above 2", Request: textgen.TextGenerationRequest{Temperature: &temperatureAboveRange}},
			{Name: "temperature at GPT-6 Sol's default medium effort", Request: textgen.TextGenerationRequest{Temperature: &temperature}},
			{Name: "temperature with low effort on GPT-6 Luna", Request: textgen.TextGenerationRequest{
				Model: "gpt-6-luna", Temperature: &temperature, ReasoningEffort: textgen.ReasoningEffortLow,
			}},
			{Name: "temperature on GPT-6 Astra", Request: textgen.TextGenerationRequest{
				Model: "gpt-6-astra", Temperature: &temperature, ReasoningEffort: textgen.ReasoningEffortLow,
			}},
			{Name: "none effort on GPT-6 Astra", Request: textgen.TextGenerationRequest{
				Model: "gpt-6-astra", ReasoningEffort: textgen.ReasoningEffortNone,
			}},
			{Name: "minimal effort on GPT-6 Sol", Request: textgen.TextGenerationRequest{ReasoningEffort: textgen.ReasoningEffortMinimal}},
			{Name: "max effort on GPT-5.5", Request: textgen.TextGenerationRequest{
				Model: "gpt-5.5", ReasoningEffort: textgen.ReasoningEffortMax,
			}},
			{Name: "low effort on a GPT-5.4 Pro snapshot", Request: textgen.TextGenerationRequest{
				Model: "gpt-5.4-pro-2026-03-05", ReasoningEffort: textgen.ReasoningEffortLow,
			}},
			{Name: "temperature with low effort on GPT-5.4 mini", Request: textgen.TextGenerationRequest{
				Model: "gpt-5.4-mini", Temperature: &temperature, ReasoningEffort: textgen.ReasoningEffortLow,
			}},
			{Name: "none effort on GPT-5", Request: textgen.TextGenerationRequest{
				Model: "gpt-5", ReasoningEffort: textgen.ReasoningEffortNone,
			}},
		},
	})
}

// TestOpenAIGenerateTextSendsTheDocumentedResponsesRequest pins the Create a model response body, including store: false.
func TestOpenAIGenerateTextSendsTheDocumentedResponsesRequest(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, responsesDialect.CredentialHeader, openAITestAPIKey)
	reply := responsesDialect.GeneratedReply(textgentest.GeneratedReply{
		Text: `{"answer":"TCP is reliable."}`, ServedModel: "gpt-6-sol", ResponseID: "resp_openai_1",
		Usage: textgen.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44},
	})
	reply.Header.Set("x-request-id", "req_openai_1")
	reply.Header.Set("x-ratelimit-remaining-requests", "499")
	reply.Header.Set("x-ratelimit-remaining-tokens", "499956")
	provider.EnqueueReplies(reply)
	temperature := 0.4
	result := runOpenAIGenerateText(t, newOpenAIClient(t, provider.BaseURL(), ""), llm.GenerateTextRequest{
		Instructions: "Answer in one sentence.",
		Messages: []textgen.Message{
			{Role: textgen.MessageRoleUser, Text: "What is TCP?"},
			{Role: textgen.MessageRoleAssistant, Text: "A transport protocol."},
			{Role: textgen.MessageRoleUser, Text: "How does it differ from UDP?"},
		},
		StructuredOutput: &textgen.StructuredOutput{Name: "answer", Description: "A one-sentence answer.", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string", "maxLength": 200}},
			"required": []any{"answer"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature, ReasoningEffort: textgen.ReasoningEffortNone,
	})

	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, `{"answer":"TCP is reliable."}`, result.Value.Text)
	require.Equal(t, "gpt-6-sol", result.Value.RequestedModel)
	require.Equal(t, "gpt-6-sol", result.Value.ServedModel)
	require.Equal(t, "resp_openai_1", result.Value.ResponseID)
	require.Equal(t, "completed", result.Value.ProviderFinishReason)
	require.Equal(t, textgen.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44}, result.Value.Usage)
	require.NotContains(t, result.Value.Text, dialectReasoningSummary)
	require.Equal(t, "openai", result.Receipt.Provider)
	require.Equal(t, "req_openai_1", result.Receipt.ProviderRequestID)
	require.Equal(t, "resp_openai_1", result.Receipt.ProviderObjectID)
	require.Equal(t, "499", result.Receipt.Metadata["x-ratelimit-remaining-requests"])
	require.Equal(t, "499956", result.Receipt.Metadata["x-ratelimit-remaining-tokens"])
	requests := provider.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].Method)
	require.Equal(t, "/responses", requests[0].Path)
	require.Equal(t, "text/event-stream", requests[0].Header.Get("Accept"))
	require.Empty(t, requests[0].Header.Get("Idempotency-Key"), "generateText sends no idempotency key")
	require.True(t, requests[0].HasCredentialInSlot, "the key travels as an Authorization bearer token")
	var body map[string]any
	require.NoError(t, json.Unmarshal(requests[0].Body, &body))
	require.Equal(t, map[string]any{
		"model":        "gpt-6-sol",
		"instructions": "Answer in one sentence.",
		"input": []any{
			map[string]any{"role": "user", "content": "What is TCP?"},
			map[string]any{"role": "assistant", "content": "A transport protocol."},
			map[string]any{"role": "user", "content": "How does it differ from UDP?"},
		},
		"max_output_tokens": float64(2048),
		"temperature":       0.4,
		"reasoning":         map[string]any{"effort": "none"},
		"text": map[string]any{"format": map[string]any{
			"type": "json_schema", "name": "answer", "description": "A one-sentence answer.", "strict": true,
			"schema": map[string]any{
				"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string", "maxLength": float64(200)}},
				"required": []any{"answer"}, "additionalProperties": false,
			},
		}},
		"store":  false,
		"stream": true,
	}, body)
}

// TestOpenAIGenerateTextAlwaysSendsStoreFalse keeps generateText a Query: a request without options still opts out of storage.
func TestOpenAIGenerateTextAlwaysSendsStoreFalse(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, responsesDialect.CredentialHeader, openAITestAPIKey)
	provider.EnqueueReplies(responsesDialect.GeneratedReply(textgentest.GeneratedReply{Text: "Hello.", ServedModel: "gpt-6-sol"}))
	result := runOpenAIGenerateText(t, newOpenAIClient(t, provider.BaseURL(), ""), openAIUserRequest("Hello"))
	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
	require.JSONEq(t, "false", string(body["store"]))
	require.JSONEq(t, "true", string(body["stream"]))
}

func TestOpenAIGenerateTextAppliesDocumentedModelFamilyRules(t *testing.T) {
	temperature := 0.3
	for _, testCase := range []struct {
		name            string
		request         llm.GenerateTextRequest
		wantEffort      string
		wantTemperature bool
		wantStreaming   bool
	}{
		{name: "GPT-5.4 accepts temperature at its default none effort", request: llm.GenerateTextRequest{
			Model: "gpt-5.4", Temperature: &temperature,
		}, wantTemperature: true, wantStreaming: true},
		{name: "GPT-6 Astra maps xhigh", request: llm.GenerateTextRequest{
			Model: "gpt-6-astra", ReasoningEffort: textgen.ReasoningEffortExtraHigh,
		}, wantEffort: "xhigh", wantStreaming: true},
		{name: "GPT-5 accepts minimal", request: llm.GenerateTextRequest{
			Model: "gpt-5-mini-2025-08-07", ReasoningEffort: textgen.ReasoningEffortMinimal,
		}, wantEffort: "minimal", wantStreaming: true},
		{name: "an undocumented model sends every field", request: llm.GenerateTextRequest{
			Model: "gpt-9-nova", Temperature: &temperature, ReasoningEffort: textgen.ReasoningEffortMax,
		}, wantEffort: "max", wantTemperature: true, wantStreaming: true},
		{name: "o3-pro does not stream", request: llm.GenerateTextRequest{
			Model: "o3-pro", ReasoningEffort: textgen.ReasoningEffortHigh,
		}, wantEffort: "high"},
		{name: "GPT-5.5 Pro does not stream", request: llm.GenerateTextRequest{
			Model: "gpt-5.5-pro", ReasoningEffort: textgen.ReasoningEffortHigh,
		}, wantEffort: "high"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, responsesDialect.CredentialHeader, openAITestAPIKey)
			generated := textgentest.GeneratedReply{Text: "Done.", ServedModel: testCase.request.Model, ResponseID: "resp_family"}
			if testCase.wantStreaming {
				provider.EnqueueReplies(responsesDialect.GeneratedReply(generated))
			} else {
				provider.EnqueueReplies(responsesDialect.UnstreamedReply(generated))
			}
			testCase.request.Messages = []textgen.Message{{Role: textgen.MessageRoleUser, Text: "Plan the migration."}}
			result := runOpenAIGenerateText(t, newOpenAIClient(t, provider.BaseURL(), ""), testCase.request)
			require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, "Done.", result.Value.Text)
			var body struct {
				Reasoning   *struct{ Effort string } `json:"reasoning"`
				Temperature *float64                 `json:"temperature"`
				Stream      bool                     `json:"stream"`
			}
			request := provider.Requests()[0]
			require.NoError(t, json.Unmarshal(request.Body, &body))
			if testCase.wantEffort == "" {
				require.Nil(t, body.Reasoning)
			} else {
				require.Equal(t, testCase.wantEffort, body.Reasoning.Effort)
			}
			require.Equal(t, testCase.wantTemperature, body.Temperature != nil)
			require.Equal(t, testCase.wantStreaming, body.Stream)
			require.Equal(t, testCase.wantStreaming, request.Header.Get("Accept") == "text/event-stream")
		})
	}
}

// TestOpenAIGenerateTextMovesUnsupportedSchemaKeywordsForFineTunedModels follows the fine-tuned Structured Outputs subset.
func TestOpenAIGenerateTextMovesUnsupportedSchemaKeywordsForFineTunedModels(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, responsesDialect.CredentialHeader, openAITestAPIKey)
	provider.EnqueueReplies(responsesDialect.GeneratedReply(textgentest.GeneratedReply{Text: `{"score":7}`, ServedModel: "ft:gpt-4.1-mini:acme::abc123"}))
	request := openAIUserRequest("Score the answer.")
	request.Model = "ft:gpt-4.1-mini:acme::abc123"
	request.StructuredOutput = &textgen.StructuredOutput{Name: "score", Schema: map[string]any{
		"type": "object", "properties": map[string]any{"score": map[string]any{"type": "integer", "minimum": 0, "maximum": 10}},
		"required": []any{"score"}, "additionalProperties": false,
	}}
	result := runOpenAIGenerateText(t, newOpenAIClient(t, provider.BaseURL(), ""), request)
	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	var body struct {
		Text struct {
			Format struct {
				Schema struct {
					Properties struct {
						Score map[string]any `json:"score"`
					} `json:"properties"`
				} `json:"schema"`
			} `json:"format"`
		} `json:"text"`
	}
	require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
	score := body.Text.Format.Schema.Properties.Score
	require.NotContains(t, score, "minimum")
	require.NotContains(t, score, "maximum")
	require.Contains(t, score["description"], "10")
}

func TestOpenAIGenerateTextStreamOutcomes(t *testing.T) {
	partial := textgentest.GeneratedReply{Text: "Partial", ServedModel: "gpt-6-sol", ResponseID: "resp_stream"}
	failed := func(code string) textgentest.FakeReply {
		response := map[string]any{
			"id": "resp_stream", "object": "response", "status": "failed", "model": "gpt-6-sol", "output": []any{},
			"error": map[string]any{"code": code, "message": "The model failed to generate a response."},
		}
		return responsesEventStreamFakeReply(responsesCreatedEvent(partial) + responsesTextDeltaEvent("Partial") + responsesEvent("response.failed", map[string]any{"response": response}))
	}
	errorEvent := func(code string) textgentest.FakeReply {
		return responsesEventStreamFakeReply(responsesCreatedEvent(partial) + responsesTextDeltaEvent("Partial") + responsesEvent("error", map[string]any{
			"code": code, "message": "Please retry your request.", "param": nil, "sequence_number": 3,
		}))
	}
	refusal := map[string]any{
		"id": "resp_stream", "object": "response", "status": "completed", "model": "gpt-6-sol",
		"output": []any{map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "status": "completed",
			"content": []any{map[string]any{"type": "refusal", "refusal": "I can't help with that."}}}},
		"usage": responsesUsage(textgen.Usage{InputTokens: 9, OutputTokens: 6, TotalTokens: 15}),
	}
	for _, testCase := range []struct {
		name       string
		reply      textgentest.FakeReply
		wantBranch sdkgo.BranchID
		wantRetry  sdkgo.FailureKind
		wantReason textgen.FinishReason
	}{
		{name: "failed server_error retries", reply: failed("server_error"), wantRetry: sdkgo.FailureAvailability},
		{name: "failed rate_limit_exceeded retries", reply: failed("rate_limit_exceeded"), wantRetry: sdkgo.FailureRateLimit},
		{name: "error event server_error retries", reply: errorEvent("server_error"), wantRetry: sdkgo.FailureAvailability},
		{name: "failed bio_policy is blocked", reply: failed("bio_policy"), wantBranch: llm.GenerateTextBranchBlocked, wantReason: textgen.FinishReasonContentPolicy},
		{name: "mid-stream misalignment block is blocked", reply: errorEvent("misalignment_policy_violation"),
			wantBranch: llm.GenerateTextBranchBlocked, wantReason: textgen.FinishReasonContentPolicy},
		{name: "mid-stream cyber_policy block is blocked", reply: errorEvent("cyber_policy"),
			wantBranch: llm.GenerateTextBranchBlocked, wantReason: textgen.FinishReasonContentPolicy},
		{name: "failed cyber_policy is blocked", reply: failed("cyber_policy"), wantBranch: llm.GenerateTextBranchBlocked, wantReason: textgen.FinishReasonContentPolicy},
		{name: "unknown error event is an invalid response", reply: errorEvent("ERR_SOMETHING"), wantBranch: llm.GenerateTextBranchInvalidResponse},
		{name: "refusal is blocked", reply: responsesEventStreamFakeReply(responsesCreatedEvent(partial) + responsesEvent("response.completed", map[string]any{"response": refusal})),
			wantBranch: llm.GenerateTextBranchBlocked, wantReason: textgen.FinishReasonRefusal},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, responsesDialect.CredentialHeader, openAITestAPIKey)
			provider.EnqueueReplies(testCase.reply)
			stepExecutionID := fmt.Sprintf("openai-stream-%d", time.Now().UnixNano())
			result, err := sdkgo.RunQuery(testsupport.NewDexContext("openai-flow", stepExecutionID),
				newOpenAIClient(t, provider.BaseURL(), "").GenerateText(), openAITestConnection, openAIUserRequest("Hello"))
			if testCase.wantRetry != "" {
				var retryError *sdkgo.RetryError
				require.ErrorAs(t, err, &retryError)
				require.Equal(t, testCase.wantRetry, retryError.Failure.Kind)
				require.NotContains(t, retryError.Failure.Message, "retry your request", "a Failure never carries provider message text")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.wantBranch, result.Branch, "failure: %+v", result.Failure)
			require.Empty(t, result.Value.Text)
			require.Equal(t, testCase.wantReason, result.Value.FinishReason)
			require.NotContains(t, result.Failure.Message, "can't help", "a Failure never carries provider message text")
		})
	}
}

// TestOpenAIGenerateTextCyberPolicyErrorIsBlocked follows OpenAI's cybersecurity safety check, which documents the
// cyber_policy error code but no HTTP status, so every status selects blocked.
func TestOpenAIGenerateTextCyberPolicyErrorIsBlocked(t *testing.T) {
	for _, statusCode := range []int{http.StatusBadRequest, http.StatusForbidden} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, responsesDialect.CredentialHeader, openAITestAPIKey)
			provider.EnqueueReplies(responsesErrorReply(statusCode, "This request was flagged as potentially high-risk cyber activity.",
				"invalid_request_error", "cyber_policy"))
			result := runOpenAIGenerateText(t, newOpenAIClient(t, provider.BaseURL(), ""), openAIUserRequest("Hello"))
			require.Equal(t, llm.GenerateTextBranchBlocked, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, textgen.FinishReasonContentPolicy, result.Value.FinishReason)
			require.Empty(t, result.Value.Text)
			require.Len(t, provider.Requests(), 1, "a safety block is not retried")
			require.NotContains(t, result.Failure.Message, "high-risk", "a Failure never carries provider message text")
		})
	}
}

func TestOpenAIGenerateTextNotImplementedIsAConclusiveRejection(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, responsesDialect.CredentialHeader, openAITestAPIKey)
	provider.EnqueueReplies(responsesErrorReply(http.StatusNotImplemented, "Not implemented.", "server_error", "server_error"))
	result := runOpenAIGenerateText(t, newOpenAIClient(t, provider.BaseURL(), ""), openAIUserRequest("Hello"))
	require.Equal(t, llm.GenerateTextBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureProviderRejection, result.Failure.Kind)
}

func TestOpenAIGenerateTextSpendLimitsAreQuotaExhausted(t *testing.T) {
	for _, code := range []string{"organization_spend_limit_exceeded", "project_spend_limit_exceeded", "organization_usage_limit_exceeded"} {
		t.Run(code, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, responsesDialect.CredentialHeader, openAITestAPIKey)
			provider.EnqueueReplies(responsesErrorReply(http.StatusTooManyRequests, "Limit reached.", "rate_limit_error", code))
			result := runOpenAIGenerateText(t, newOpenAIClient(t, provider.BaseURL(), ""), openAIUserRequest("Hello"))
			require.Equal(t, llm.GenerateTextBranchProviderRejected, result.Branch)
			require.Equal(t, sdkgo.FailureQuotaExhausted, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, code)
		})
	}
}

func newOpenAIQuery(t testing.TB, connection textgentest.FakeConnection) *textgen.TextGenerationQuery {
	return newOpenAIFakeProviderClient(t, connection).GenerateText()
}

// newOpenAIFakeProviderClient builds a Client for a textgentest suite's fake provider and connection.
func newOpenAIFakeProviderClient(t testing.TB, connection textgentest.FakeConnection) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderOpenai, Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[llm.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		llm.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newOpenAIClient(t *testing.T, baseURL string, model string) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderOpenai, Model: model}, openAITestCredentials(), llm.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func openAITestCredentials() sdkgo.StaticCredentialProvider[llm.Credentials] {
	return sdkgo.StaticCredentialProvider[llm.Credentials]{
		openAITestConnection: {APIKey: sdkgo.NewSecretString(openAITestAPIKey)},
	}
}

func runOpenAIGenerateText(t *testing.T, client *llm.Client, request llm.GenerateTextRequest) llm.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("openai-generate-text-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("openai-flow", stepExecutionID), client.GenerateText(), openAITestConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), openAITestAPIKey), "a Result contains the API key")
	return result
}

func openAIUserRequest(text string) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: text}}}
}
