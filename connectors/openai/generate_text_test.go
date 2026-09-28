// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package openai_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	openai "github.com/superdurable/dex-connectors-library/connectors/openai"
	"github.com/superdurable/dex-connectors-library/connectors/openai/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
)

const generateTextAPIKey = "sk-proj-openai-connector-generate-text-key"

var generateTextConnection = sdkgo.ConnectionRef{Provider: "openai", Name: "openai-generate-text"}

func TestGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange, temperature := 2.1, 0.4
	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
		Dialect:  responsesDialect,
		NewQuery: newGenerateTextQuery,
		LocallyRejectedRequests: []llmtest.NamedTextGenerationRequest{
			{Name: "temperature above 2", Request: llm.TextGenerationRequest{Temperature: &temperatureAboveRange}},
			{Name: "temperature at GPT-6 Sol's default medium effort", Request: llm.TextGenerationRequest{Temperature: &temperature}},
			{Name: "temperature with low effort on GPT-6 Luna", Request: llm.TextGenerationRequest{
				Model: "gpt-6-luna", Temperature: &temperature, ReasoningEffort: llm.ReasoningEffortLow,
			}},
			{Name: "temperature on GPT-6 Astra", Request: llm.TextGenerationRequest{
				Model: "gpt-6-astra", Temperature: &temperature, ReasoningEffort: llm.ReasoningEffortLow,
			}},
			{Name: "none effort on GPT-6 Astra", Request: llm.TextGenerationRequest{
				Model: "gpt-6-astra", ReasoningEffort: llm.ReasoningEffortNone,
			}},
			{Name: "minimal effort on GPT-6 Sol", Request: llm.TextGenerationRequest{ReasoningEffort: llm.ReasoningEffortMinimal}},
			{Name: "max effort on GPT-5.5", Request: llm.TextGenerationRequest{
				Model: "gpt-5.5", ReasoningEffort: llm.ReasoningEffortMax,
			}},
			{Name: "low effort on a GPT-5.4 Pro snapshot", Request: llm.TextGenerationRequest{
				Model: "gpt-5.4-pro-2026-03-05", ReasoningEffort: llm.ReasoningEffortLow,
			}},
			{Name: "temperature with low effort on GPT-5.4 mini", Request: llm.TextGenerationRequest{
				Model: "gpt-5.4-mini", Temperature: &temperature, ReasoningEffort: llm.ReasoningEffortLow,
			}},
			{Name: "none effort on GPT-5", Request: llm.TextGenerationRequest{
				Model: "gpt-5", ReasoningEffort: llm.ReasoningEffortNone,
			}},
		},
	})
}

// TestGenerateTextSendsTheDocumentedResponsesRequest pins the Create a model response body, including store: false.
func TestGenerateTextSendsTheDocumentedResponsesRequest(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, responsesDialect.CredentialHeader, generateTextAPIKey)
	reply := responsesDialect.GeneratedReply(llmtest.GeneratedReply{
		Text: `{"answer":"TCP is reliable."}`, ServedModel: "gpt-6-sol", ResponseID: "resp_openai_1",
		Usage: llm.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44},
	})
	reply.Header.Set("x-request-id", "req_openai_1")
	reply.Header.Set("x-ratelimit-remaining-requests", "499")
	reply.Header.Set("x-ratelimit-remaining-tokens", "499956")
	provider.EnqueueReplies(reply)
	temperature := 0.4
	result := runGenerateText(t, newGenerateTextClient(t, provider.BaseURL(), ""), openai.GenerateTextRequest{
		Instructions: "Answer in one sentence.",
		Messages: []llm.Message{
			{Role: llm.MessageRoleUser, Text: "What is TCP?"},
			{Role: llm.MessageRoleAssistant, Text: "A transport protocol."},
			{Role: llm.MessageRoleUser, Text: "How does it differ from UDP?"},
		},
		StructuredOutput: &llm.StructuredOutput{Name: "answer", Description: "A one-sentence answer.", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string", "maxLength": 200}},
			"required": []any{"answer"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature, ReasoningEffort: llm.ReasoningEffortNone,
	})

	require.Equal(t, openai.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, `{"answer":"TCP is reliable."}`, result.Value.Text)
	require.Equal(t, "gpt-6-sol", result.Value.RequestedModel)
	require.Equal(t, "gpt-6-sol", result.Value.ServedModel)
	require.Equal(t, "resp_openai_1", result.Value.ResponseID)
	require.Equal(t, "completed", result.Value.ProviderFinishReason)
	require.Equal(t, llm.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44}, result.Value.Usage)
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

// TestGenerateTextAlwaysSendsStoreFalse keeps generateText a Query: a request without options still opts out of storage.
func TestGenerateTextAlwaysSendsStoreFalse(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, responsesDialect.CredentialHeader, generateTextAPIKey)
	provider.EnqueueReplies(responsesDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Hello.", ServedModel: "gpt-6-sol"}))
	result := runGenerateText(t, newGenerateTextClient(t, provider.BaseURL(), ""), generateTextUserRequest("Hello"))
	require.Equal(t, openai.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
	require.JSONEq(t, "false", string(body["store"]))
	require.JSONEq(t, "true", string(body["stream"]))
}

func TestGenerateTextAppliesDocumentedModelFamilyRules(t *testing.T) {
	temperature := 0.3
	for _, testCase := range []struct {
		name            string
		request         openai.GenerateTextRequest
		wantEffort      string
		wantTemperature bool
		wantStreaming   bool
	}{
		{name: "GPT-5.4 accepts temperature at its default none effort", request: openai.GenerateTextRequest{
			Model: "gpt-5.4", Temperature: &temperature,
		}, wantTemperature: true, wantStreaming: true},
		{name: "GPT-6 Astra maps xhigh", request: openai.GenerateTextRequest{
			Model: "gpt-6-astra", ReasoningEffort: llm.ReasoningEffortExtraHigh,
		}, wantEffort: "xhigh", wantStreaming: true},
		{name: "GPT-5 accepts minimal", request: openai.GenerateTextRequest{
			Model: "gpt-5-mini-2025-08-07", ReasoningEffort: llm.ReasoningEffortMinimal,
		}, wantEffort: "minimal", wantStreaming: true},
		{name: "an undocumented model sends every field", request: openai.GenerateTextRequest{
			Model: "gpt-9-nova", Temperature: &temperature, ReasoningEffort: llm.ReasoningEffortMax,
		}, wantEffort: "max", wantTemperature: true, wantStreaming: true},
		{name: "o3-pro does not stream", request: openai.GenerateTextRequest{
			Model: "o3-pro", ReasoningEffort: llm.ReasoningEffortHigh,
		}, wantEffort: "high"},
		{name: "GPT-5.5 Pro does not stream", request: openai.GenerateTextRequest{
			Model: "gpt-5.5-pro", ReasoningEffort: llm.ReasoningEffortHigh,
		}, wantEffort: "high"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, responsesDialect.CredentialHeader, generateTextAPIKey)
			generated := llmtest.GeneratedReply{Text: "Done.", ServedModel: testCase.request.Model, ResponseID: "resp_family"}
			if testCase.wantStreaming {
				provider.EnqueueReplies(responsesDialect.GeneratedReply(generated))
			} else {
				provider.EnqueueReplies(responsesDialect.UnstreamedReply(generated))
			}
			testCase.request.Messages = []llm.Message{{Role: llm.MessageRoleUser, Text: "Plan the migration."}}
			result := runGenerateText(t, newGenerateTextClient(t, provider.BaseURL(), ""), testCase.request)
			require.Equal(t, openai.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
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

// TestGenerateTextMovesUnsupportedSchemaKeywordsForFineTunedModels follows the fine-tuned Structured Outputs subset.
func TestGenerateTextMovesUnsupportedSchemaKeywordsForFineTunedModels(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, responsesDialect.CredentialHeader, generateTextAPIKey)
	provider.EnqueueReplies(responsesDialect.GeneratedReply(llmtest.GeneratedReply{Text: `{"score":7}`, ServedModel: "ft:gpt-4.1-mini:acme::abc123"}))
	request := generateTextUserRequest("Score the answer.")
	request.Model = "ft:gpt-4.1-mini:acme::abc123"
	request.StructuredOutput = &llm.StructuredOutput{Name: "score", Schema: map[string]any{
		"type": "object", "properties": map[string]any{"score": map[string]any{"type": "integer", "minimum": 0, "maximum": 10}},
		"required": []any{"score"}, "additionalProperties": false,
	}}
	result := runGenerateText(t, newGenerateTextClient(t, provider.BaseURL(), ""), request)
	require.Equal(t, openai.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
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

func TestGenerateTextStreamOutcomes(t *testing.T) {
	partial := llmtest.GeneratedReply{Text: "Partial", ServedModel: "gpt-6-sol", ResponseID: "resp_stream"}
	failed := func(code string) llmtest.FakeReply {
		response := map[string]any{
			"id": "resp_stream", "object": "response", "status": "failed", "model": "gpt-6-sol", "output": []any{},
			"error": map[string]any{"code": code, "message": "The model failed to generate a response."},
		}
		return eventStreamReply(createdEvent(partial) + textDeltaEvent("Partial") + responsesEvent("response.failed", map[string]any{"response": response}))
	}
	errorEvent := func(code string) llmtest.FakeReply {
		return eventStreamReply(createdEvent(partial) + textDeltaEvent("Partial") + responsesEvent("error", map[string]any{
			"code": code, "message": "Please retry your request.", "param": nil, "sequence_number": 3,
		}))
	}
	refusal := map[string]any{
		"id": "resp_stream", "object": "response", "status": "completed", "model": "gpt-6-sol",
		"output": []any{map[string]any{"id": "msg_1", "type": "message", "role": "assistant", "status": "completed",
			"content": []any{map[string]any{"type": "refusal", "refusal": "I can't help with that."}}}},
		"usage": responsesUsage(llm.Usage{InputTokens: 9, OutputTokens: 6, TotalTokens: 15}),
	}
	for _, testCase := range []struct {
		name       string
		reply      llmtest.FakeReply
		wantBranch sdkgo.BranchID
		wantRetry  sdkgo.FailureKind
		wantReason llm.FinishReason
	}{
		{name: "failed server_error retries", reply: failed("server_error"), wantRetry: sdkgo.FailureAvailability},
		{name: "failed rate_limit_exceeded retries", reply: failed("rate_limit_exceeded"), wantRetry: sdkgo.FailureRateLimit},
		{name: "error event server_error retries", reply: errorEvent("server_error"), wantRetry: sdkgo.FailureAvailability},
		{name: "failed bio_policy is blocked", reply: failed("bio_policy"), wantBranch: openai.GenerateTextBranchBlocked, wantReason: llm.FinishReasonContentPolicy},
		{name: "mid-stream misalignment block is blocked", reply: errorEvent("misalignment_policy_violation"),
			wantBranch: openai.GenerateTextBranchBlocked, wantReason: llm.FinishReasonContentPolicy},
		{name: "mid-stream cyber_policy block is blocked", reply: errorEvent("cyber_policy"),
			wantBranch: openai.GenerateTextBranchBlocked, wantReason: llm.FinishReasonContentPolicy},
		{name: "failed cyber_policy is blocked", reply: failed("cyber_policy"), wantBranch: openai.GenerateTextBranchBlocked, wantReason: llm.FinishReasonContentPolicy},
		{name: "unknown error event is an invalid response", reply: errorEvent("ERR_SOMETHING"), wantBranch: openai.GenerateTextBranchInvalidResponse},
		{name: "refusal is blocked", reply: eventStreamReply(createdEvent(partial) + responsesEvent("response.completed", map[string]any{"response": refusal})),
			wantBranch: openai.GenerateTextBranchBlocked, wantReason: llm.FinishReasonRefusal},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, responsesDialect.CredentialHeader, generateTextAPIKey)
			provider.EnqueueReplies(testCase.reply)
			stepExecutionID := fmt.Sprintf("openai-stream-%d", time.Now().UnixNano())
			result, err := sdkgo.RunQuery(testsupport.NewDexContext("openai-flow", stepExecutionID),
				newGenerateTextClient(t, provider.BaseURL(), "").GenerateText(), generateTextConnection, generateTextUserRequest("Hello"))
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

// TestGenerateTextCyberPolicyErrorIsBlocked follows OpenAI's cybersecurity safety check, which documents the
// cyber_policy error code but no HTTP status, so every status selects blocked.
func TestGenerateTextCyberPolicyErrorIsBlocked(t *testing.T) {
	for _, statusCode := range []int{http.StatusBadRequest, http.StatusForbidden} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, responsesDialect.CredentialHeader, generateTextAPIKey)
			provider.EnqueueReplies(responsesErrorReply(statusCode, "This request was flagged as potentially high-risk cyber activity.",
				"invalid_request_error", "cyber_policy"))
			result := runGenerateText(t, newGenerateTextClient(t, provider.BaseURL(), ""), generateTextUserRequest("Hello"))
			require.Equal(t, openai.GenerateTextBranchBlocked, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, llm.FinishReasonContentPolicy, result.Value.FinishReason)
			require.Empty(t, result.Value.Text)
			require.Len(t, provider.Requests(), 1, "a safety block is not retried")
			require.NotContains(t, result.Failure.Message, "high-risk", "a Failure never carries provider message text")
		})
	}
}

func TestGenerateTextNotImplementedIsAConclusiveRejection(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, responsesDialect.CredentialHeader, generateTextAPIKey)
	provider.EnqueueReplies(responsesErrorReply(http.StatusNotImplemented, "Not implemented.", "server_error", "server_error"))
	result := runGenerateText(t, newGenerateTextClient(t, provider.BaseURL(), ""), generateTextUserRequest("Hello"))
	require.Equal(t, openai.GenerateTextBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureProviderRejection, result.Failure.Kind)
}

func TestGenerateTextSpendLimitsAreQuotaExhausted(t *testing.T) {
	for _, code := range []string{"organization_spend_limit_exceeded", "project_spend_limit_exceeded", "organization_usage_limit_exceeded"} {
		t.Run(code, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, responsesDialect.CredentialHeader, generateTextAPIKey)
			provider.EnqueueReplies(responsesErrorReply(http.StatusTooManyRequests, "Limit reached.", "rate_limit_error", code))
			result := runGenerateText(t, newGenerateTextClient(t, provider.BaseURL(), ""), generateTextUserRequest("Hello"))
			require.Equal(t, openai.GenerateTextBranchProviderRejected, result.Branch)
			require.Equal(t, sdkgo.FailureQuotaExhausted, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, code)
		})
	}
}

// TestGenerateTextCallsTheOpenAIResponsesEndpoint proves the production URL without a network call.
func TestGenerateTextCallsTheOpenAIResponsesEndpoint(t *testing.T) {
	reply := responsesDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Hello.", ServedModel: "gpt-6-sol"})
	var requestedURLs []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestedURLs = append(requestedURLs, request.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK, Header: reply.Header.Clone(), Body: io.NopCloser(strings.NewReader(reply.Body)), Request: request,
		}, nil
	})
	client, err := openai.New(openai.Config{}, generateTextCredentials(), openai.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	result := runGenerateText(t, client, generateTextUserRequest("Hello"))
	require.Equal(t, openai.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "gpt-6-sol", result.Value.RequestedModel, "a blank connection model uses the manifest default")
	require.Equal(t, []string{"https://api.openai.com/v1/responses"}, requestedURLs)
}

func TestNewRejectsInvalidGenerateTextConfiguration(t *testing.T) {
	_, err := openai.New(openai.Config{Model: "gpt 6"}, generateTextCredentials())
	require.Error(t, err, "an invalid connection model fails at startup")
	_, err = openai.New(openai.Config{Endpoint: "http://api.openai.example"}, generateTextCredentials())
	require.ErrorContains(t, err, "HTTPS")
	_, err = openai.New(openai.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = openai.New(openai.Config{}, generateTextCredentials(), nil)
	require.ErrorContains(t, err, "option is nil")
}

// TestNewCapsOnlyGenerateTextAtALongClientTimeout keeps a v0.6.0 createResponse client timeout valid.
func TestNewCapsOnlyGenerateTextAtALongClientTimeout(t *testing.T) {
	reply := responsesDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Hello.", ServedModel: "gpt-6-sol"})
	remainingByPath := map[string]time.Duration{}
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		deadline, hasDeadline := request.Context().Deadline()
		require.True(t, hasDeadline, "every exchange has a client deadline")
		remainingByPath[request.URL.Path] = time.Until(deadline)
		body := reply.Body
		header := reply.Header.Clone()
		if request.Header.Get("Idempotency-Key") != "" {
			body = `{"id":"resp_1","model":"gpt-6-sol","status":"completed","output":[],"usage":{}}`
			header = http.Header{"Content-Type": {"application/json"}}
		}
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	callerHTTPClient := &http.Client{Transport: transport, Timeout: 15 * time.Minute}
	client, err := openai.New(openai.Config{}, generateTextCredentials(), openai.WithHTTPClient(callerHTTPClient))
	require.NoError(t, err, "a createResponse timeout above generateText's Execute timeout does not fail New")
	require.Equal(t, 15*time.Minute, callerHTTPClient.Timeout, "the caller's client is not modified")

	result := runGenerateText(t, client, generateTextUserRequest("Hello"))
	require.Equal(t, openai.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.LessOrEqual(t, remainingByPath["/v1/responses"], 870*time.Second, "generateText is capped below its Execute timeout")

	delete(remainingByPath, "/v1/responses")
	created, err := sdkgo.RunMutation(testsupport.NewDexContext("openai-flow", fmt.Sprintf("openai-create-%d", time.Now().UnixNano())),
		client.CreateResponse(), generateTextConnection, openai.CreateRequest{Model: "gpt-6-sol", Input: "Hello"})
	require.NoError(t, err)
	require.Equal(t, openai.CreateResponseBranchCompleted, created.Branch, "failure: %+v", created.Failure)
	require.Greater(t, remainingByPath["/v1/responses"], 14*time.Minute, "createResponse keeps the caller's timeout")
}

// TestNewKeepsVersion060EndpointsForCreateResponse keeps endpoints v0.6.0 accepted; only generateText refuses them.
func TestNewKeepsVersion060EndpointsForCreateResponse(t *testing.T) {
	for _, testCase := range []struct{ name, endpoint, wantReason string }{
		{name: "user information", endpoint: "https://gateway-user:gateway-secret@gateway.example/v1", wantReason: "user information"},
		{name: "query", endpoint: "https://gateway.example/v1?gateway-secret=1", wantReason: "query"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var requestedHosts []string
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requestedHosts = append(requestedHosts, request.URL.Host)
				return &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Request: request,
					Body: io.NopCloser(strings.NewReader(`{"id":"resp_1","model":"gpt-6-sol","status":"completed","output":[],"usage":{}}`)),
				}, nil
			})
			client, err := openai.New(openai.Config{Endpoint: testCase.endpoint}, generateTextCredentials(),
				openai.WithHTTPClient(&http.Client{Transport: transport}))
			require.NoError(t, err, "v0.6.0 accepted this endpoint")

			result := runGenerateText(t, client, generateTextUserRequest("Hello"))
			require.Equal(t, openai.GenerateTextBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, testCase.wantReason)
			require.NotContains(t, result.Failure.Message, "gateway-secret", "a Failure never repeats the endpoint")
			require.Empty(t, requestedHosts, "generateText sends no request, not even to the default endpoint")

			created, err := sdkgo.RunMutation(testsupport.NewDexContext("openai-flow", fmt.Sprintf("openai-create-%d", time.Now().UnixNano())),
				client.CreateResponse(), generateTextConnection, openai.CreateRequest{Model: "gpt-6-sol", Input: "Hello"})
			require.NoError(t, err)
			require.Equal(t, openai.CreateResponseBranchCompleted, created.Branch, "failure: %+v", created.Failure)
			require.Equal(t, []string{"gateway.example"}, requestedHosts, "createResponse still calls the configured endpoint")
		})
	}
}

func newGenerateTextQuery(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
	return newFakeProviderClient(t, connection).GenerateText()
}

// newFakeProviderClient builds a Client for an llmtest suite's fake provider and connection.
func newFakeProviderClient(t testing.TB, connection llmtest.FakeConnection) *openai.Client {
	t.Helper()
	client, err := openai.New(openai.Config{
		Model: connection.Model, Endpoint: connection.BaseURL, MaxResponseBytes: connection.MaxResponseBytes,
	}, sdkgo.StaticCredentialProvider[openai.Credentials]{connection.Reference: {APIKey: connection.APIKey}})
	require.NoError(t, err)
	return client
}

func newGenerateTextClient(t *testing.T, endpoint string, model string) *openai.Client {
	t.Helper()
	client, err := openai.New(openai.Config{Model: model, Endpoint: endpoint}, generateTextCredentials())
	require.NoError(t, err)
	return client
}

func generateTextCredentials() sdkgo.StaticCredentialProvider[openai.Credentials] {
	return sdkgo.StaticCredentialProvider[openai.Credentials]{
		generateTextConnection: {APIKey: sdkgo.NewSecretString(generateTextAPIKey)},
	}
}

func runGenerateText(t *testing.T, client *openai.Client, request openai.GenerateTextRequest) openai.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("openai-generate-text-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("openai-flow", stepExecutionID), client.GenerateText(), generateTextConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), generateTextAPIKey), "a Result contains the API key")
	return result
}

func generateTextUserRequest(text string) openai.GenerateTextRequest {
	return openai.GenerateTextRequest{Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: text}}}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
