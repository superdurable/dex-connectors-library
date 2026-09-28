// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kimi_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/moonshot/kimi"
	"github.com/superdurable/dex-connectors-library/connectors/moonshot/kimi/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat/openaichattest"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "sk-kimi-connector-test-key-0123456789"

var testConnection = sdkgo.ConnectionRef{Provider: "moonshot", Name: "kimi-test"}

// kimiDialect matches chatProfile: streamed replies, the 429 quota type, and the 400 content_filter type.
var kimiDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "kimi-k2.6", AlternateModel: "kimi-k3", IsStreaming: true,
	QuotaExhaustedStatusCode: http.StatusTooManyRequests, QuotaExhaustedErrorToken: "exceeded_current_quota_error",
	ContentPolicyErrorToken: "content_filter",
})

func TestGenerateTextFollowsTheExchangeContract(t *testing.T) {
	fixedTemperature, lowTemperature := 1.0, 0.2
	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
		Dialect:  kimiDialect,
		NewQuery: newKimiQuery,
		LocallyRejectedRequests: []llmtest.NamedTextGenerationRequest{
			{Name: "temperature on kimi-k2.6", Request: llm.TextGenerationRequest{Temperature: &lowTemperature}},
			{Name: "the fixed temperature on kimi-k3", Request: llm.TextGenerationRequest{
				Model: "kimi-k3", Temperature: &fixedTemperature,
			}},
			{Name: "reasoning effort on kimi-k2.6, which uses a thinking switch", Request: llm.TextGenerationRequest{
				ReasoningEffort: llm.ReasoningEffortHigh,
			}},
			{Name: "reasoning effort on kimi-k2.7-code", Request: llm.TextGenerationRequest{
				Model: "kimi-k2.7-code", ReasoningEffort: llm.ReasoningEffortLow,
			}},
			{Name: "reasoning effort none, because kimi-k3 always reasons", Request: llm.TextGenerationRequest{
				Model: "kimi-k3", ReasoningEffort: llm.ReasoningEffortNone,
			}},
			{Name: "medium reasoning effort, which kimi-k3 does not offer", Request: llm.TextGenerationRequest{
				Model: "kimi-k3", ReasoningEffort: llm.ReasoningEffortMedium,
			}},
			{Name: "extra-high reasoning effort on kimi-k3", Request: llm.TextGenerationRequest{
				Model: "kimi-k3", ReasoningEffort: llm.ReasoningEffortExtraHigh,
			}},
		},
	})
}

// TestGenerateTextSendsTheDocumentedChatCompletionsRequest pins the body fields Kimi documents for Chat Completions.
func TestGenerateTextSendsTheDocumentedChatCompletionsRequest(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, kimiDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(kimiDialect.GeneratedReply(llmtest.GeneratedReply{
		Text: `{"answer":"TCP is reliable.","confidence":9}`, ServedModel: "kimi-k3", ResponseID: "cmpl-kimi-1",
		Usage: llm.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, TotalTokens: 44},
	}))
	result := runGenerateText(t, newKimiClient(t, provider.BaseURL(), ""), kimi.GenerateTextRequest{
		Model:        "kimi-k3",
		Instructions: "Answer in one sentence.",
		Messages:     []llm.Message{{Role: llm.MessageRoleUser, Text: "How does TCP differ from UDP?"}},
		StructuredOutput: &llm.StructuredOutput{Name: "answer", Schema: map[string]any{
			"type": "object", "title": "Answer",
			"properties": map[string]any{
				"answer":     map[string]any{"type": "string", "maxLength": 200},
				"confidence": map[string]any{"type": "integer", "minimum": 0, "maximum": 10},
			},
			"required": []any{"answer", "confidence"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, ReasoningEffort: llm.ReasoningEffortHigh,
	})

	require.Equal(t, kimi.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.JSONEq(t, `{"answer":"TCP is reliable.","confidence":9}`, result.Value.Text)
	require.Equal(t, "kimi-k3", result.Value.RequestedModel)
	require.Equal(t, int64(5), result.Value.Usage.CachedInputTokens)
	require.Equal(t, "kimi", result.Receipt.Provider)
	require.Equal(t, "cmpl-kimi-1", result.Receipt.ProviderObjectID)
	requests := provider.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].Method)
	require.Equal(t, "/chat/completions", requests[0].Path)
	require.Equal(t, "text/event-stream", requests[0].Header.Get("Accept"))
	require.True(t, requests[0].HasCredentialInSlot, "the key travels as an Authorization bearer token")
	var body map[string]any
	require.NoError(t, json.Unmarshal(requests[0].Body, &body))
	require.Equal(t, map[string]any{
		"model": "kimi-k3",
		"messages": []any{
			map[string]any{"role": "system", "content": "Answer in one sentence."},
			map[string]any{"role": "user", "content": "How does TCP differ from UDP?"},
		},
		"max_completion_tokens": float64(2048),
		"reasoning_effort":      "high",
		"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{
			"name": "answer", "strict": true, "schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"answer":     map[string]any{"type": "string", "description": "(maxLength: 200)"},
					"confidence": map[string]any{"type": "integer", "description": "(maximum: 10, minimum: 0)"},
				},
				"required": []any{"answer", "confidence"}, "additionalProperties": false,
			},
		}},
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}, body)
}

// TestGenerateTextMapsEveryKimiK3ReasoningEffort keeps the three efforts Kimi K3 documents.
func TestGenerateTextMapsEveryKimiK3ReasoningEffort(t *testing.T) {
	for effort, wireValue := range map[llm.ReasoningEffort]string{
		llm.ReasoningEffortLow: "low", llm.ReasoningEffortHigh: "high", llm.ReasoningEffortMax: "max",
	} {
		t.Run(string(effort), func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, kimiDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(kimiDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Done.", ServedModel: "kimi-k3"}))
			result := runGenerateText(t, newKimiClient(t, provider.BaseURL(), "kimi-k3"), kimi.GenerateTextRequest{
				Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: "Plan the migration."}}, ReasoningEffort: effort,
			})
			require.Equal(t, kimi.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			var body struct {
				ReasoningEffort string `json:"reasoning_effort"`
			}
			require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
			require.Equal(t, wireValue, body.ReasoningEffort)
		})
	}
}

// TestTooManyRequestsIsClassifiedByErrorType separates Kimi's three documented 429 types.
func TestTooManyRequestsIsClassifiedByErrorType(t *testing.T) {
	t.Run("exhausted balance is a conclusive rejection", func(t *testing.T) {
		provider := llmtest.NewFakeProvider(t, kimiDialect.CredentialHeader, testAPIKey)
		provider.EnqueueReplies(kimiErrorReply(http.StatusTooManyRequests, "exceeded_current_quota_error",
			"Account balance is insufficient or the account has been disabled", ""))
		result := runGenerateText(t, newKimiClient(t, provider.BaseURL(), ""), userRequest("Hello"))
		require.Equal(t, kimi.GenerateTextBranchProviderRejected, result.Branch)
		require.Equal(t, sdkgo.FailureQuotaExhausted, result.Failure.Kind)
		require.Contains(t, result.Failure.Message, "exceeded_current_quota_error")
		require.NotContains(t, result.Failure.Message, "balance", "a Failure never carries provider message text")
		require.Len(t, provider.Requests(), 1)
	})
	for _, testCase := range []struct {
		name, errorType string
		kind            sdkgo.FailureKind
	}{
		{"engine overload", "engine_overloaded_error", sdkgo.FailureAvailability},
		{"organization rate limit", "rate_limit_reached_error", sdkgo.FailureRateLimit},
	} {
		t.Run(testCase.name+" returns Retry after Retry-After", func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, kimiDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(kimiErrorReply(http.StatusTooManyRequests, testCase.errorType, "Please try again later", "3"))
			retryError, retryAfter := requireRetry(t, newKimiClient(t, provider.BaseURL(), ""), userRequest("Hello"))
			require.Equal(t, testCase.kind, retryError.Failure.Kind)
			require.Contains(t, retryError.Failure.Message, testCase.errorType)
			require.Equal(t, 3*time.Second, retryAfter)
		})
	}
}

// TestContentFilterRejectionSelectsBlocked maps Kimi's 400 content safety rejection to the shared blocked branch.
func TestContentFilterRejectionSelectsBlocked(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, kimiDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(kimiErrorReply(http.StatusBadRequest, "content_filter",
		"The request was rejected because it was considered high risk", ""))
	result := runGenerateText(t, newKimiClient(t, provider.BaseURL(), ""), userRequest("Hello"))
	require.Equal(t, kimi.GenerateTextBranchBlocked, result.Branch)
	require.Equal(t, llm.FinishReasonContentPolicy, result.Value.FinishReason)
	require.Empty(t, result.Value.Text)
}

// TestStreamedErrorObjectsClassifyLikeTheirHTTPForm keeps a mid-stream error on the branch its HTTP form selects.
func TestStreamedErrorObjectsClassifyLikeTheirHTTPForm(t *testing.T) {
	for _, testCase := range []struct {
		errorType string
		kind      sdkgo.FailureKind
	}{
		{"engine_overloaded_error", sdkgo.FailureAvailability},
		{"rate_limit_reached_error", sdkgo.FailureRateLimit},
		{"server_error", sdkgo.FailureAvailability},
		{"unexpected_output", sdkgo.FailureAvailability},
		{"server_unavailable", sdkgo.FailureAvailability},
	} {
		t.Run(testCase.errorType+" returns Retry", func(t *testing.T) {
			provider := llmtest.NewFakeProvider(t, kimiDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(streamedErrorReply(testCase.errorType))
			retryError, _ := requireRetry(t, newKimiClient(t, provider.BaseURL(), ""), userRequest("Hello"))
			require.Equal(t, testCase.kind, retryError.Failure.Kind)
			require.NotContains(t, retryError.Failure.Message, "Please retry", "a Failure never carries provider message text")
		})
	}
	t.Run("content_filter selects blocked", func(t *testing.T) {
		provider := llmtest.NewFakeProvider(t, kimiDialect.CredentialHeader, testAPIKey)
		provider.EnqueueReplies(streamedErrorReply("content_filter"))
		result := runGenerateText(t, newKimiClient(t, provider.BaseURL(), ""), userRequest("Hello"))
		require.Equal(t, kimi.GenerateTextBranchBlocked, result.Branch)
		require.Empty(t, result.Value.Text, "the partial text before the error is not returned")
	})
	t.Run("exceeded_current_quota_error selects providerRejected", func(t *testing.T) {
		provider := llmtest.NewFakeProvider(t, kimiDialect.CredentialHeader, testAPIKey)
		provider.EnqueueReplies(streamedErrorReply("exceeded_current_quota_error"))
		result := runGenerateText(t, newKimiClient(t, provider.BaseURL(), ""), userRequest("Hello"))
		require.Equal(t, kimi.GenerateTextBranchProviderRejected, result.Branch)
		require.Equal(t, sdkgo.FailureQuotaExhausted, result.Failure.Kind)
	})
}

// TestNotImplementedIsAConclusiveRejection keeps 501 out of the server_error retry rule.
func TestNotImplementedIsAConclusiveRejection(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, kimiDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(kimiErrorReply(http.StatusNotImplemented, "server_error", "Not implemented.", ""))
	result := runGenerateText(t, newKimiClient(t, provider.BaseURL(), ""), userRequest("Hello"))
	require.Equal(t, kimi.GenerateTextBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureProviderRejection, result.Failure.Kind)
}

// TestGenerateTextCallsTheConfiguredKimiPlatform proves both production URLs without a network call.
func TestGenerateTextCallsTheConfiguredKimiPlatform(t *testing.T) {
	for _, testCase := range []struct {
		name, endpoint, expectedURL string
	}{
		{"blank endpoint uses the global platform", "", "https://api.moonshot.ai/v1/chat/completions"},
		{"global platform", "https://api.moonshot.ai/v1", "https://api.moonshot.ai/v1/chat/completions"},
		{"China platform", "https://api.moonshot.cn/v1", "https://api.moonshot.cn/v1/chat/completions"},
		{"China platform with a trailing slash", "https://api.moonshot.cn/v1/", "https://api.moonshot.cn/v1/chat/completions"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			reply := kimiDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Hello.", ServedModel: "kimi-k2.6"})
			var requestedURLs []string
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requestedURLs = append(requestedURLs, request.URL.String())
				return &http.Response{
					StatusCode: http.StatusOK, Header: reply.Header.Clone(), Body: io.NopCloser(strings.NewReader(reply.Body)), Request: request,
				}, nil
			})
			client, err := kimi.New(kimi.Config{Endpoint: testCase.endpoint}, testCredentials(),
				kimi.WithHTTPClient(&http.Client{Transport: transport}))
			require.NoError(t, err)
			result := runGenerateText(t, client, userRequest("Hello"))
			require.Equal(t, kimi.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, "kimi-k2.6", result.Value.RequestedModel, "a blank connection model uses the manifest default")
			require.Equal(t, []string{testCase.expectedURL}, requestedURLs)
		})
	}
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	for _, endpoint := range []string{
		"http://api.moonshot.ai/v1", "https://api.moonshot.ai", "https://api.moonshot.ai/v1/chat",
		"https://api.moonshot.ai.example.com/v1", "https://platform.kimi.ai/v1", "https://api.moonshot.ai/anthropic",
		"http://127.0.0.1:8080/v1",
	} {
		_, err := kimi.New(kimi.Config{Endpoint: endpoint}, testCredentials())
		require.ErrorContains(t, err, "endpoint", endpoint)
	}
	for _, baseURL := range []string{"https://api.moonshot.ai.example.com", "http://192.0.2.10:8080", "https://attacker.example"} {
		_, err := kimi.New(kimi.Config{}, testCredentials(), kimi.WithBaseURLForTest(baseURL))
		require.ErrorContains(t, err, "loopback", baseURL)
	}
	for _, baseURL := range []string{"http://127.0.0.1:8080", "http://localhost:9000", "http://[::1]:7000"} {
		_, err := kimi.New(kimi.Config{}, testCredentials(), kimi.WithBaseURLForTest(baseURL))
		require.NoError(t, err, baseURL)
	}
	_, err := kimi.New(kimi.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = kimi.New(kimi.Config{Model: "kimi-k2.6 é"}, testCredentials())
	require.Error(t, err, "an invalid connection model fails at startup")
	_, err = kimi.New(kimi.Config{MaxResponseBytes: -1}, testCredentials())
	require.ErrorContains(t, err, "maxResponseBytes")
	_, err = kimi.New(kimi.Config{}, testCredentials(), nil)
	require.ErrorContains(t, err, "option is nil")
	// The README says the exchange bound cannot reach the 900-second Execute timeout, so a longer override cannot help.
	_, err = kimi.New(kimi.Config{}, testCredentials(), kimi.WithHTTPClient(&http.Client{Timeout: 900 * time.Second}))
	require.ErrorContains(t, err, "Execute timeout must exceed the request timeout")
	_, err = kimi.New(kimi.Config{}, testCredentials(), kimi.WithHTTPClient(&http.Client{Timeout: 899 * time.Second}))
	require.NoError(t, err)
}

func newKimiQuery(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
	return newFakeProviderClient(t, connection).GenerateText()
}

// newFakeProviderClient builds a Client for an llmtest suite's fake provider and connection.
func newFakeProviderClient(t testing.TB, connection llmtest.FakeConnection) *kimi.Client {
	t.Helper()
	client, err := kimi.New(kimi.Config{Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[kimi.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		kimi.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newKimiClient(t *testing.T, baseURL string, model string) *kimi.Client {
	t.Helper()
	client, err := kimi.New(kimi.Config{Model: model}, testCredentials(), kimi.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func testCredentials() sdkgo.StaticCredentialProvider[kimi.Credentials] {
	return sdkgo.StaticCredentialProvider[kimi.Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
}

func runGenerateText(t *testing.T, client *kimi.Client, request kimi.GenerateTextRequest) kimi.GenerateTextResult {
	t.Helper()
	result, err := sdkgo.RunQuery(newDexContext(), client.GenerateText(), testConnection, request)
	require.NoError(t, err)
	requireNoAPIKey(t, result)
	return result
}

// requireRetry runs request and returns its Retry and the provider-requested delay, or zero when the Step policy applies.
func requireRetry(t *testing.T, client *kimi.Client, request kimi.GenerateTextRequest) (*sdkgo.RetryError, time.Duration) {
	t.Helper()
	_, err := sdkgo.RunQuery(newDexContext(), client.GenerateText(), testConnection, request)
	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError)
	require.False(t, strings.Contains(fmt.Sprintf("%+v", err), testAPIKey), "a Retry contains the API key")
	var retryAfter *dex.RetryAfterError
	if !errors.As(err, &retryAfter) {
		return retryError, 0
	}
	return retryError, retryAfter.After
}

func newDexContext() *testsupport.DexContext {
	return testsupport.NewDexContext("kimi-flow", fmt.Sprintf("kimi-step-%d", time.Now().UnixNano()))
}

func requireNoAPIKey(t *testing.T, result kimi.GenerateTextResult) {
	t.Helper()
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	// A boolean assertion keeps the key out of the failure message.
	require.False(t, strings.Contains(string(encoded)+fmt.Sprintf("%+v", result), testAPIKey), "a Result contains the API key")
}

// kimiErrorReply renders Kimi's documented error envelope, which carries only error.type and error.message.
func kimiErrorReply(statusCode int, errorType string, message string, retryAfter string) llmtest.FakeReply {
	header := http.Header{"Content-Type": {"application/json"}}
	if retryAfter != "" {
		header.Set("Retry-After", retryAfter)
	}
	return llmtest.FakeReply{
		StatusCode: statusCode, Header: header,
		Body: fmt.Sprintf(`{"error":{"message":%q,"type":%q}}`, message, errorType),
	}
}

// streamedErrorReply sends one content delta and then an error object in place of the next chunk.
func streamedErrorReply(errorType string) llmtest.FakeReply {
	return llmtest.FakeReply{
		Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: `data: {"id":"cmpl-kimi-2","object":"chat.completion.chunk","created":1790000000,"model":"kimi-k2.6",` +
			`"choices":[{"index":0,"delta":{"role":"assistant","content":"Partial"},"finish_reason":null}]}` + "\n\n" +
			fmt.Sprintf(`data: {"error":{"message":"Please retry your request.","type":%q}}`, errorType) + "\n\n",
	}
}

func userRequest(text string) kimi.GenerateTextRequest {
	return kimi.GenerateTextRequest{Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: text}}}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
