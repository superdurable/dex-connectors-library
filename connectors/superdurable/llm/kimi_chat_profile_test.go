// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"encoding/json"
	"errors"
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

const kimiTestAPIKey = "sk-kimi-connector-test-key-0123456789"

var kimiTestConnection = sdkgo.ConnectionRef{Provider: "llm", Name: "kimi-test"}

// kimiDialect matches chatProfile: streamed replies, the 429 quota type, and the 400 content_filter type.
var kimiDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "kimi-k2.6", AlternateModel: "kimi-k3", IsStreaming: true,
	QuotaExhaustedStatusCode: http.StatusTooManyRequests, QuotaExhaustedErrorToken: "exceeded_current_quota_error",
	ContentPolicyErrorToken: "content_filter",
})

func TestKimiGenerateTextFollowsTheExchangeContract(t *testing.T) {
	fixedTemperature, lowTemperature := 1.0, 0.2
	textgentest.RunTextGenerationExchangeSuite(t, &textgentest.TextGenerationExchangeSuite{
		Dialect:  kimiDialect,
		NewQuery: newKimiQuery,
		LocallyRejectedRequests: []textgentest.NamedTextGenerationRequest{
			{Name: "temperature on kimi-k2.6", Request: textgen.TextGenerationRequest{Temperature: &lowTemperature}},
			{Name: "the fixed temperature on kimi-k3", Request: textgen.TextGenerationRequest{
				Model: "kimi-k3", Temperature: &fixedTemperature,
			}},
			{Name: "reasoning effort on kimi-k2.6, which uses a thinking switch", Request: textgen.TextGenerationRequest{
				ReasoningEffort: textgen.ReasoningEffortHigh,
			}},
			{Name: "reasoning effort on kimi-k2.7-code", Request: textgen.TextGenerationRequest{
				Model: "kimi-k2.7-code", ReasoningEffort: textgen.ReasoningEffortLow,
			}},
			{Name: "reasoning effort none, because kimi-k3 always reasons", Request: textgen.TextGenerationRequest{
				Model: "kimi-k3", ReasoningEffort: textgen.ReasoningEffortNone,
			}},
			{Name: "medium reasoning effort, which kimi-k3 does not offer", Request: textgen.TextGenerationRequest{
				Model: "kimi-k3", ReasoningEffort: textgen.ReasoningEffortMedium,
			}},
			{Name: "extra-high reasoning effort on kimi-k3", Request: textgen.TextGenerationRequest{
				Model: "kimi-k3", ReasoningEffort: textgen.ReasoningEffortExtraHigh,
			}},
		},
	})
}

// TestKimiGenerateTextSendsTheDocumentedChatCompletionsRequest pins the body fields Kimi documents for Chat Completions.
func TestKimiGenerateTextSendsTheDocumentedChatCompletionsRequest(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, kimiDialect.CredentialHeader, kimiTestAPIKey)
	provider.EnqueueReplies(kimiDialect.GeneratedReply(textgentest.GeneratedReply{
		Text: `{"answer":"TCP is reliable.","confidence":9}`, ServedModel: "kimi-k3", ResponseID: "cmpl-kimi-1",
		Usage: textgen.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, TotalTokens: 44},
	}))
	result := runKimiGenerateText(t, newKimiClient(t, provider.BaseURL(), ""), llm.GenerateTextRequest{
		Model:        "kimi-k3",
		Instructions: "Answer in one sentence.",
		Messages:     []textgen.Message{{Role: textgen.MessageRoleUser, Text: "How does TCP differ from UDP?"}},
		StructuredOutput: &textgen.StructuredOutput{Name: "answer", Schema: map[string]any{
			"type": "object", "title": "Answer",
			"properties": map[string]any{
				"answer":     map[string]any{"type": "string", "maxLength": 200},
				"confidence": map[string]any{"type": "integer", "minimum": 0, "maximum": 10},
			},
			"required": []any{"answer", "confidence"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, ReasoningEffort: textgen.ReasoningEffortHigh,
	})

	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
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

// TestKimiGenerateTextMapsEveryKimiK3ReasoningEffort keeps the three efforts Kimi K3 documents.
func TestKimiGenerateTextMapsEveryKimiK3ReasoningEffort(t *testing.T) {
	for effort, wireValue := range map[textgen.ReasoningEffort]string{
		textgen.ReasoningEffortLow: "low", textgen.ReasoningEffortHigh: "high", textgen.ReasoningEffortMax: "max",
	} {
		t.Run(string(effort), func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, kimiDialect.CredentialHeader, kimiTestAPIKey)
			provider.EnqueueReplies(kimiDialect.GeneratedReply(textgentest.GeneratedReply{Text: "Done.", ServedModel: "kimi-k3"}))
			result := runKimiGenerateText(t, newKimiClient(t, provider.BaseURL(), "kimi-k3"), llm.GenerateTextRequest{
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

// TestKimiTooManyRequestsIsClassifiedByErrorType separates Kimi's three documented 429 types.
func TestKimiTooManyRequestsIsClassifiedByErrorType(t *testing.T) {
	t.Run("exhausted balance is a conclusive rejection", func(t *testing.T) {
		provider := textgentest.NewFakeProvider(t, kimiDialect.CredentialHeader, kimiTestAPIKey)
		provider.EnqueueReplies(kimiErrorReply(http.StatusTooManyRequests, "exceeded_current_quota_error",
			"Account balance is insufficient or the account has been disabled", ""))
		result := runKimiGenerateText(t, newKimiClient(t, provider.BaseURL(), ""), kimiUserRequest("Hello"))
		require.Equal(t, llm.GenerateTextBranchProviderRejected, result.Branch)
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
			provider := textgentest.NewFakeProvider(t, kimiDialect.CredentialHeader, kimiTestAPIKey)
			provider.EnqueueReplies(kimiErrorReply(http.StatusTooManyRequests, testCase.errorType, "Please try again later", "3"))
			retryError, retryAfter := requireKimiRetry(t, newKimiClient(t, provider.BaseURL(), ""), kimiUserRequest("Hello"))
			require.Equal(t, testCase.kind, retryError.Failure.Kind)
			require.Contains(t, retryError.Failure.Message, testCase.errorType)
			require.Equal(t, 3*time.Second, retryAfter)
		})
	}
}

// TestKimiContentFilterRejectionSelectsBlocked maps Kimi's 400 content safety rejection to the shared blocked branch.
func TestKimiContentFilterRejectionSelectsBlocked(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, kimiDialect.CredentialHeader, kimiTestAPIKey)
	provider.EnqueueReplies(kimiErrorReply(http.StatusBadRequest, "content_filter",
		"The request was rejected because it was considered high risk", ""))
	result := runKimiGenerateText(t, newKimiClient(t, provider.BaseURL(), ""), kimiUserRequest("Hello"))
	require.Equal(t, llm.GenerateTextBranchBlocked, result.Branch)
	require.Equal(t, textgen.FinishReasonContentPolicy, result.Value.FinishReason)
	require.Empty(t, result.Value.Text)
}

// TestKimiStreamedErrorObjectsClassifyLikeTheirHTTPForm keeps a mid-stream error on the branch its HTTP form selects.
func TestKimiStreamedErrorObjectsClassifyLikeTheirHTTPForm(t *testing.T) {
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
			provider := textgentest.NewFakeProvider(t, kimiDialect.CredentialHeader, kimiTestAPIKey)
			provider.EnqueueReplies(kimiStreamedErrorReply(testCase.errorType))
			retryError, _ := requireKimiRetry(t, newKimiClient(t, provider.BaseURL(), ""), kimiUserRequest("Hello"))
			require.Equal(t, testCase.kind, retryError.Failure.Kind)
			require.NotContains(t, retryError.Failure.Message, "Please retry", "a Failure never carries provider message text")
		})
	}
	t.Run("content_filter selects blocked", func(t *testing.T) {
		provider := textgentest.NewFakeProvider(t, kimiDialect.CredentialHeader, kimiTestAPIKey)
		provider.EnqueueReplies(kimiStreamedErrorReply("content_filter"))
		result := runKimiGenerateText(t, newKimiClient(t, provider.BaseURL(), ""), kimiUserRequest("Hello"))
		require.Equal(t, llm.GenerateTextBranchBlocked, result.Branch)
		require.Empty(t, result.Value.Text, "the partial text before the error is not returned")
	})
	t.Run("exceeded_current_quota_error selects providerRejected", func(t *testing.T) {
		provider := textgentest.NewFakeProvider(t, kimiDialect.CredentialHeader, kimiTestAPIKey)
		provider.EnqueueReplies(kimiStreamedErrorReply("exceeded_current_quota_error"))
		result := runKimiGenerateText(t, newKimiClient(t, provider.BaseURL(), ""), kimiUserRequest("Hello"))
		require.Equal(t, llm.GenerateTextBranchProviderRejected, result.Branch)
		require.Equal(t, sdkgo.FailureQuotaExhausted, result.Failure.Kind)
	})
}

// TestKimiNotImplementedIsAConclusiveRejection keeps 501 out of the server_error retry rule.
func TestKimiNotImplementedIsAConclusiveRejection(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, kimiDialect.CredentialHeader, kimiTestAPIKey)
	provider.EnqueueReplies(kimiErrorReply(http.StatusNotImplemented, "server_error", "Not implemented.", ""))
	result := runKimiGenerateText(t, newKimiClient(t, provider.BaseURL(), ""), kimiUserRequest("Hello"))
	require.Equal(t, llm.GenerateTextBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureProviderRejection, result.Failure.Kind)
}

func newKimiQuery(t testing.TB, connection textgentest.FakeConnection) *textgen.TextGenerationQuery {
	return newKimiFakeProviderClient(t, connection).GenerateText()
}

// newKimiFakeProviderClient builds a Client for a textgentest suite's fake provider and connection.
func newKimiFakeProviderClient(t testing.TB, connection textgentest.FakeConnection) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderKimi, Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[llm.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		llm.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newKimiClient(t *testing.T, baseURL string, model string) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderKimi, Model: model}, kimiTestCredentials(), llm.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func kimiTestCredentials() sdkgo.StaticCredentialProvider[llm.Credentials] {
	return sdkgo.StaticCredentialProvider[llm.Credentials]{kimiTestConnection: {APIKey: sdkgo.NewSecretString(kimiTestAPIKey)}}
}

func runKimiGenerateText(t *testing.T, client *llm.Client, request llm.GenerateTextRequest) llm.GenerateTextResult {
	t.Helper()
	result, err := sdkgo.RunQuery(newKimiDexContext(), client.GenerateText(), kimiTestConnection, request)
	require.NoError(t, err)
	requireNoKimiAPIKey(t, result)
	return result
}

// requireKimiRetry runs request and returns its Retry and the provider-requested delay, or zero when the Step policy applies.
func requireKimiRetry(t *testing.T, client *llm.Client, request llm.GenerateTextRequest) (*sdkgo.RetryError, time.Duration) {
	t.Helper()
	_, err := sdkgo.RunQuery(newKimiDexContext(), client.GenerateText(), kimiTestConnection, request)
	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError)
	require.False(t, strings.Contains(fmt.Sprintf("%+v", err), kimiTestAPIKey), "a Retry contains the API key")
	var retryAfter *dex.RetryAfterError
	if !errors.As(err, &retryAfter) {
		return retryError, 0
	}
	return retryError, retryAfter.After
}

func newKimiDexContext() *testsupport.DexContext {
	return testsupport.NewDexContext("kimi-flow", fmt.Sprintf("kimi-step-%d", time.Now().UnixNano()))
}

func requireNoKimiAPIKey(t *testing.T, result llm.GenerateTextResult) {
	t.Helper()
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	// A boolean assertion keeps the key out of the failure message.
	require.False(t, strings.Contains(string(encoded)+fmt.Sprintf("%+v", result), kimiTestAPIKey), "a Result contains the API key")
}

// kimiErrorReply renders Kimi's documented error envelope, which carries only error.type and error.message.
func kimiErrorReply(statusCode int, errorType string, message string, retryAfter string) textgentest.FakeReply {
	header := http.Header{"Content-Type": {"application/json"}}
	if retryAfter != "" {
		header.Set("Retry-After", retryAfter)
	}
	return textgentest.FakeReply{
		StatusCode: statusCode, Header: header,
		Body: fmt.Sprintf(`{"error":{"message":%q,"type":%q}}`, message, errorType),
	}
}

// kimiStreamedErrorReply sends one content delta and then an error object in place of the next chunk.
func kimiStreamedErrorReply(errorType string) textgentest.FakeReply {
	return textgentest.FakeReply{
		Header: http.Header{"Content-Type": {"text/event-stream"}},
		Body: `data: {"id":"cmpl-kimi-2","object":"chat.completion.chunk","created":1790000000,"model":"kimi-k2.6",` +
			`"choices":[{"index":0,"delta":{"role":"assistant","content":"Partial"},"finish_reason":null}]}` + "\n\n" +
			fmt.Sprintf(`data: {"error":{"message":"Please retry your request.","type":%q}}`, errorType) + "\n\n",
	}
}

func kimiUserRequest(text string) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: text}}}
}
