// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package grok_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	grok "github.com/superdurable/dex-connectors-library/connectors/xai"
	"github.com/superdurable/dex-connectors-library/connectors/xai/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
)

const testAPIKey = "xai-grok-connector-test-key-4f2a9d"

var testConnection = sdkgo.ConnectionRef{Provider: "xai", Name: "grok-test"}

// xaiDialect matches chatProfile and xAI's reasoning-token accounting.
var xaiDialect = testsupport.NewXAIProviderDialect("grok-4.3", "grok-4.7")

func TestGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange := 2.1
	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
		Dialect:  xaiDialect,
		NewQuery: newGrokQuery,
		LocallyRejectedRequests: []llmtest.NamedTextGenerationRequest{
			{Name: "temperature above 2", Request: llm.TextGenerationRequest{Temperature: &temperatureAboveRange}},
			{Name: "reasoning effort none on grok-4.7, which always reasons", Request: llm.TextGenerationRequest{
				Model: "grok-4.7", ReasoningEffort: llm.ReasoningEffortNone,
			}},
			{Name: "minimal reasoning effort, which xAI does not offer", Request: llm.TextGenerationRequest{
				ReasoningEffort: llm.ReasoningEffortMinimal,
			}},
			{Name: "max reasoning effort, which xAI does not offer", Request: llm.TextGenerationRequest{
				Model: "grok-4.7", ReasoningEffort: llm.ReasoningEffortMax,
			}},
			{Name: "xhigh reasoning effort on grok-4.5, which xAI serves as high", Request: llm.TextGenerationRequest{
				Model: "grok-4.5", ReasoningEffort: llm.ReasoningEffortExtraHigh,
			}},
			{Name: "reasoning effort on a Grok 4.20 alias", Request: llm.TextGenerationRequest{
				Model: "grok-4.20-reasoning", ReasoningEffort: llm.ReasoningEffortLow,
			}},
			{Name: "reasoning effort on grok-build-0.1", Request: llm.TextGenerationRequest{
				Model: "grok-build-0.1", ReasoningEffort: llm.ReasoningEffortHigh,
			}},
		},
	})
}

// TestGenerateTextSendsTheDocumentedChatCompletionsRequest pins the body fields xAI documents for Chat Completions.
func TestGenerateTextSendsTheDocumentedChatCompletionsRequest(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, xaiDialect.CredentialHeader, testAPIKey)
	// The dialect sends completion_tokens 9 beside reasoning_tokens 94, as xAI's API reference does.
	contractUsage := llm.Usage{InputTokens: 32, CachedInputTokens: 6, OutputTokens: 103, ReasoningTokens: 94, TotalTokens: 135}
	provider.EnqueueReplies(xaiDialect.GeneratedReply(llmtest.GeneratedReply{
		Text: `{"answer":"TCP is reliable."}`, ServedModel: "grok-4.7", ResponseID: "0daf962f-a275-4a3c-839a-047854645532",
		Usage: contractUsage,
	}))
	temperature := 0.4
	result := runGenerateText(t, newGrokClient(t, provider.BaseURL(), "grok-4.7"), grok.GenerateTextRequest{
		Instructions: "Answer in one sentence.",
		Messages:     []llm.Message{{Role: llm.MessageRoleUser, Text: "How does TCP differ from UDP?"}},
		StructuredOutput: &llm.StructuredOutput{Name: "answer", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
			"required": []any{"answer"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature, ReasoningEffort: llm.ReasoningEffortExtraHigh,
	})

	require.Equal(t, grok.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, `{"answer":"TCP is reliable."}`, result.Value.Text)
	require.Equal(t, "grok-4.7", result.Value.RequestedModel)
	require.Equal(t, contractUsage, result.Value.Usage)
	require.Equal(t, "grok", result.Receipt.Provider)
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

// TestGenerateTextSendsEachModelsDocumentedReasoningEffort covers the per-model effort tables in profile.go.
func TestGenerateTextSendsEachModelsDocumentedReasoningEffort(t *testing.T) {
	for _, testCase := range []struct {
		model     string
		effort    llm.ReasoningEffort
		wireValue string
	}{
		{"grok-4.3", llm.ReasoningEffortNone, "none"},
		{"grok-4.3-latest", llm.ReasoningEffortExtraHigh, "xhigh"},
		{"grok-4.7", llm.ReasoningEffortLow, "low"},
		{"grok-4.6", llm.ReasoningEffortExtraHigh, "xhigh"},
		{"grok-4.5", llm.ReasoningEffortHigh, "high"},
		{"grok-build-latest", llm.ReasoningEffortMedium, "medium"},
	} {
		t.Run(testCase.model+"/"+string(testCase.effort), func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, xaiDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(xaiDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Done.", ServedModel: testCase.model}))
			result := runGenerateText(t, newGrokClient(t, provider.BaseURL(), testCase.model), grok.GenerateTextRequest{
				Messages:        []llm.Message{{Role: llm.MessageRoleUser, Text: "Plan the migration."}},
				ReasoningEffort: testCase.effort,
			})
			require.Equal(t, grok.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			var body struct {
				ReasoningEffort string `json:"reasoning_effort"`
			}
			require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
			require.Equal(t, testCase.wireValue, body.ReasoningEffort)
		})
	}
}

// TestUsageCountsReasoningInOutputTokens uses xAI's API reference usage in a stream and a complete body.
func TestUsageCountsReasoningInOutputTokens(t *testing.T) {
	const xaiUsage = `"usage":{"prompt_tokens":32,"completion_tokens":9,"total_tokens":135,` +
		`"prompt_tokens_details":{"text_tokens":32,"cached_tokens":6},"completion_tokens_details":{"reasoning_tokens":94},` +
		`"cost_in_usd_ticks":1234000}`
	for _, testCase := range []struct {
		name  string
		reply llmtest.FakeReply
	}{
		{"event stream", streamReply(
			`{"id":"chatcmpl-xai-1","object":"chat.completion.chunk","model":"grok-4.3","choices":[{"index":0,"delta":{"role":"assistant","content":"303"},"finish_reason":"stop"}],`+xaiUsage+`}`,
			"[DONE]",
		)},
		{"complete body from a gateway that ignored streaming", llmtest.FakeReply{
			Header: http.Header{"Content-Type": {"application/json"}},
			Body: `{"id":"chatcmpl-xai-1","object":"chat.completion","model":"grok-4.3",` +
				`"choices":[{"index":0,"message":{"role":"assistant","content":"303"},"finish_reason":"stop"}],` + xaiUsage + `}`,
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, xaiDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(testCase.reply)
			result := runGenerateText(t, newGrokClient(t, provider.BaseURL(), ""), userRequest("What is 101*3?"))
			require.Equal(t, grok.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, "303", result.Value.Text)
			require.Equal(t, llm.Usage{InputTokens: 32, CachedInputTokens: 6, OutputTokens: 103, ReasoningTokens: 94, TotalTokens: 135},
				result.Value.Usage, "OutputTokens includes the 94 reasoning tokens beside xAI's 9 completion tokens")
		})
	}
}

// TestUsageCountsReasoningOnce adds reasoning to OutputTokens unless the total shows it is already there.
func TestUsageCountsReasoningOnce(t *testing.T) {
	for _, testCase := range []struct {
		name, usage string
		expected    llm.Usage
	}{
		{
			"completion tokens that already include reasoning, as OpenAI reports them",
			`{"prompt_tokens":32,"completion_tokens":103,"total_tokens":135,"completion_tokens_details":{"reasoning_tokens":94}}`,
			llm.Usage{InputTokens: 32, OutputTokens: 103, ReasoningTokens: 94, TotalTokens: 135},
		},
		{
			"no reasoning tokens, as in xAI's deferred-completion example",
			`{"prompt_tokens":31,"completion_tokens":11,"total_tokens":42,"completion_tokens_details":{"reasoning_tokens":0}}`,
			llm.Usage{InputTokens: 31, OutputTokens: 11, TotalTokens: 42},
		},
		{
			"a total with other billed tokens keeps xAI's accounting",
			`{"prompt_tokens":32,"completion_tokens":9,"total_tokens":200,"completion_tokens_details":{"reasoning_tokens":94}}`,
			llm.Usage{InputTokens: 32, OutputTokens: 103, ReasoningTokens: 94, TotalTokens: 200},
		},
		{
			"no reported total keeps xAI's accounting",
			`{"prompt_tokens":32,"completion_tokens":9,"completion_tokens_details":{"reasoning_tokens":94}}`,
			llm.Usage{InputTokens: 32, OutputTokens: 103, ReasoningTokens: 94, TotalTokens: 135},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, xaiDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(streamReply(
				`{"id":"chatcmpl-xai-4","object":"chat.completion.chunk","model":"grok-4.3","choices":[{"index":0,"delta":{"role":"assistant","content":"303"},"finish_reason":"stop"}],"usage":`+testCase.usage+`}`,
				"[DONE]",
			))
			result := runGenerateText(t, newGrokClient(t, provider.BaseURL(), ""), userRequest("What is 101*3?"))
			require.Equal(t, grok.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, testCase.expected, result.Value.Usage)
		})
	}
}

// TestEndTurnFinishSelectsGenerated accepts the end_turn finish token the API reference lists beside stop.
func TestEndTurnFinishSelectsGenerated(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, xaiDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(streamReply(
		`{"id":"chatcmpl-xai-2","object":"chat.completion.chunk","model":"grok-4.3","choices":[{"index":0,"delta":{"role":"assistant","content":"Hello."},"finish_reason":null}]}`,
		`{"id":"chatcmpl-xai-2","object":"chat.completion.chunk","model":"grok-4.3","choices":[{"index":0,"delta":{},"finish_reason":"end_turn"}]}`,
		"[DONE]",
	))
	result := runGenerateText(t, newGrokClient(t, provider.BaseURL(), ""), userRequest("Hi"))
	require.Equal(t, grok.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, llm.FinishReasonStop, result.Value.FinishReason)
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
			grok.GenerateTextBranchProviderRejected, sdkgo.FailureProviderRejection, "invalid-argument", "Incorrect API key",
		},
		{
			"missing credentials", http.StatusUnauthorized,
			`{"code":"unauthenticated:no-credentials","error":"No credentials presented."}`,
			grok.GenerateTextBranchProviderRejected, sdkgo.FailureAuthentication, "unauthenticated:no-credentials", "No credentials",
		},
		{
			"model missing on the US endpoint", http.StatusNotFound,
			`{"code":"not-found","error":"The model grok-4.3 does not exist or your team team-1 does not have access to it."}`,
			grok.GenerateTextBranchProviderRejected, sdkgo.FailureNotFound, "not-found", "does not exist",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, xaiDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(llmtest.FakeReply{
				StatusCode: testCase.statusCode, Header: http.Header{"Content-Type": {"application/json"}}, Body: testCase.body,
			})
			result := runGenerateText(t, newGrokClient(t, provider.BaseURL(), ""), userRequest("Hello"))
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, testCase.errorCode, "the /code token identifies the error")
			require.NotContains(t, result.Failure.Message, testCase.message, "a Failure never carries provider message text")
		})
	}
}

// TestMidStreamErrorObjectSelectsInvalidResponse keeps an undocumented xAI stream error out of Retry and out of the Failure.
func TestMidStreamErrorObjectSelectsInvalidResponse(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, xaiDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(streamReply(
		`{"id":"chatcmpl-xai-3","object":"chat.completion.chunk","model":"grok-4.3","choices":[{"index":0,"delta":{"role":"assistant","content":"Partial"},"finish_reason":null}]}`,
		`{"code":"internal","error":"Please retry your request."}`,
	))
	result := runGenerateText(t, newGrokClient(t, provider.BaseURL(), ""), userRequest("Hello"))
	require.Equal(t, grok.GenerateTextBranchInvalidResponse, result.Branch)
	require.Empty(t, result.Value.Text)
	require.Contains(t, result.Failure.Message, "internal")
	require.NotContains(t, result.Failure.Message, "Please retry", "a Failure never carries provider message text")
}

// TestGenerateTextCallsTheConfiguredXAIEndpoint proves both production URLs without a network call.
func TestGenerateTextCallsTheConfiguredXAIEndpoint(t *testing.T) {
	for _, testCase := range []struct{ endpoint, requestURL string }{
		{"", "https://api.x.ai/v1/chat/completions"},
		{"https://api.x.ai/v1/", "https://api.x.ai/v1/chat/completions"},
		{"https://us.api.x.ai/v1", "https://us.api.x.ai/v1/chat/completions"},
	} {
		t.Run(testCase.requestURL, func(t *testing.T) {
			reply := xaiDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Hello.", ServedModel: "grok-4.7"})
			var requestedURLs []string
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requestedURLs = append(requestedURLs, request.URL.String())
				return &http.Response{
					StatusCode: http.StatusOK, Header: reply.Header.Clone(), Body: io.NopCloser(strings.NewReader(reply.Body)), Request: request,
				}, nil
			})
			client, err := grok.New(grok.Config{Endpoint: testCase.endpoint}, testCredentials(),
				grok.WithHTTPClient(&http.Client{Transport: transport}))
			require.NoError(t, err)
			result := runGenerateText(t, client, userRequest("Hello"))
			require.Equal(t, grok.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, "grok-4.3", result.Value.RequestedModel, "a blank connection model uses the manifest default")
			require.Equal(t, []string{testCase.requestURL}, requestedURLs)
		})
	}
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	for _, endpoint := range []string{
		"https://api.x.ai", "https://api.x.ai/v2", "https://api.x.ai/v1/chat", "http://api.x.ai/v1",
		"https://eu.api.x.ai/v1", "https://api.x.ai.example.com/v1", "https://attacker.example/v1", "http://127.0.0.1:8080",
	} {
		_, err := grok.New(grok.Config{Endpoint: endpoint}, testCredentials())
		require.ErrorContains(t, err, "endpoint must be https://api.x.ai/v1 or https://us.api.x.ai/v1", endpoint)
	}
	for _, baseURL := range []string{"https://api.x.ai.example.com", "http://192.0.2.10:8080", "https://attacker.example", ""} {
		_, err := grok.New(grok.Config{}, testCredentials(), grok.WithBaseURLForTest(baseURL))
		require.ErrorContains(t, err, "loopback", baseURL)
	}
	for _, baseURL := range []string{"http://127.0.0.1:8080", "http://localhost:9000", "http://[::1]:7000"} {
		_, err := grok.New(grok.Config{Endpoint: "https://us.api.x.ai/v1"}, testCredentials(), grok.WithBaseURLForTest(baseURL))
		require.NoError(t, err, baseURL)
	}
	_, err := grok.New(grok.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = grok.New(grok.Config{Model: "grok-é"}, testCredentials())
	require.Error(t, err, "an invalid connection model fails at startup")
	_, err = grok.New(grok.Config{MaxResponseBytes: -1}, testCredentials())
	require.ErrorContains(t, err, "maxResponseBytes")
	_, err = grok.New(grok.Config{}, testCredentials(), nil)
	require.ErrorContains(t, err, "option is nil")
}

func newGrokQuery(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
	return newFakeProviderClient(t, connection).GenerateText()
}

// newFakeProviderClient builds a Client for an llmtest suite's fake provider and connection.
func newFakeProviderClient(t testing.TB, connection llmtest.FakeConnection) *grok.Client {
	t.Helper()
	client, err := grok.New(grok.Config{Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[grok.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		grok.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newGrokClient(t *testing.T, baseURL string, model string) *grok.Client {
	t.Helper()
	client, err := grok.New(grok.Config{Model: model}, testCredentials(), grok.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func testCredentials() sdkgo.StaticCredentialProvider[grok.Credentials] {
	return sdkgo.StaticCredentialProvider[grok.Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
}

func runGenerateText(t *testing.T, client *grok.Client, request grok.GenerateTextRequest) grok.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("grok-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("grok-flow", stepExecutionID), client.GenerateText(), testConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), testAPIKey), "a Result contains the API key")
	return result
}

func userRequest(text string) grok.GenerateTextRequest {
	return grok.GenerateTextRequest{Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: text}}}
}

// streamReply renders each data payload as one server-sent event.
func streamReply(dataPayloads ...string) llmtest.FakeReply {
	var body strings.Builder
	for _, data := range dataPayloads {
		body.WriteString("data: " + data + "\n\n")
	}
	return llmtest.FakeReply{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body.String()}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
