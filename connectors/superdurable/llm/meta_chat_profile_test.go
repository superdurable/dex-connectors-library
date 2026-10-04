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
)

const metaTestAPIKey = "LLM|1234567890|meta-connector-test-key"

var metaTestConnection = sdkgo.ConnectionRef{Provider: "llm", Name: "meta-test"}

// metaDialect matches chatProfile: streamed replies, the default 402 quota error, and Meta's 400 content-policy code.
var metaDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "muse-spark-1.3", AlternateModel: "muse-spark-1.2", IsStreaming: true,
	ContentPolicyErrorToken: "content_policy_violation",
})

func TestMetaGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange := 2.1
	textgentest.RunTextGenerationExchangeSuite(t, &textgentest.TextGenerationExchangeSuite{
		Dialect:  metaDialect,
		NewQuery: newMetaQuery,
		LocallyRejectedRequests: []textgentest.NamedTextGenerationRequest{
			{Name: "temperature above 2", Request: textgen.TextGenerationRequest{Temperature: &temperatureAboveRange}},
			{Name: "reasoning effort none, which Muse Spark rejects", Request: textgen.TextGenerationRequest{
				ReasoningEffort: textgen.ReasoningEffortNone,
			}},
			{Name: "max reasoning effort on muse-spark-1.2", Request: textgen.TextGenerationRequest{
				Model: "muse-spark-1.2", ReasoningEffort: textgen.ReasoningEffortMax,
			}},
			{Name: "max reasoning effort on the Contributor tier", Request: textgen.TextGenerationRequest{
				Model: "muse-spark-1.3-contributor", ReasoningEffort: textgen.ReasoningEffortMax,
			}},
		},
	})
}

// TestMetaGenerateTextSendsTheDocumentedChatCompletionsRequest pins the body fields Meta documents for Chat Completions.
func TestMetaGenerateTextSendsTheDocumentedChatCompletionsRequest(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, metaDialect.CredentialHeader, metaTestAPIKey)
	reply := metaDialect.GeneratedReply(textgentest.GeneratedReply{
		Text: `{"answer":"TCP is reliable."}`, ServedModel: "muse-spark-1.3", ResponseID: "chatcmpl-meta-1",
		Usage: textgen.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44},
	})
	reply.Header.Set("x-ratelimit-remaining-requests", "2999")
	reply.Header.Set("x-ratelimit-remaining-tokens", "3999956")
	provider.EnqueueReplies(reply)
	temperature := 0.4
	result := runMetaGenerateText(t, newMetaClient(t, provider.BaseURL(), ""), llm.GenerateTextRequest{
		Instructions: "Answer in one sentence.",
		Messages:     []textgen.Message{{Role: textgen.MessageRoleUser, Text: "How does TCP differ from UDP?"}},
		StructuredOutput: &textgen.StructuredOutput{Name: "answer", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
			"required": []any{"answer"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature, ReasoningEffort: textgen.ReasoningEffortMax,
	})

	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, `{"answer":"TCP is reliable."}`, result.Value.Text)
	require.Equal(t, "muse-spark-1.3", result.Value.RequestedModel)
	require.Equal(t, int64(12), result.Value.Usage.ReasoningTokens)
	require.Equal(t, "meta", result.Receipt.Provider)
	require.Equal(t, "2999", result.Receipt.Metadata["x-ratelimit-remaining-requests"])
	require.Equal(t, "3999956", result.Receipt.Metadata["x-ratelimit-remaining-tokens"])
	requests := provider.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].Method)
	require.Equal(t, "/v1/chat/completions", requests[0].Path)
	require.Equal(t, "text/event-stream", requests[0].Header.Get("Accept"))
	require.True(t, requests[0].HasCredentialInSlot, "the key travels as an Authorization bearer token")
	var body map[string]any
	require.NoError(t, json.Unmarshal(requests[0].Body, &body))
	require.Equal(t, map[string]any{
		"model": "muse-spark-1.3",
		"messages": []any{
			map[string]any{"role": "developer", "content": "Answer in one sentence."},
			map[string]any{"role": "user", "content": "How does TCP differ from UDP?"},
		},
		"max_completion_tokens": float64(2048),
		"temperature":           0.4,
		"reasoning_effort":      "max",
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

// TestMetaGenerateTextMapsExtraHighReasoningToXHigh keeps the one effort whose wire value differs from its name.
func TestMetaGenerateTextMapsExtraHighReasoningToXHigh(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, metaDialect.CredentialHeader, metaTestAPIKey)
	provider.EnqueueReplies(metaDialect.GeneratedReply(textgentest.GeneratedReply{Text: "Done.", ServedModel: "muse-spark-1.2"}))
	result := runMetaGenerateText(t, newMetaClient(t, provider.BaseURL(), "muse-spark-1.2"), llm.GenerateTextRequest{
		Messages:        []textgen.Message{{Role: textgen.MessageRoleUser, Text: "Plan the migration."}},
		ReasoningEffort: textgen.ReasoningEffortExtraHigh,
	})
	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	var body struct {
		ReasoningEffort string `json:"reasoning_effort"`
	}
	require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
	require.Equal(t, "xhigh", body.ReasoningEffort)
}

// TestMetaMidStreamServerErrorsReturnRetry follows Meta's guidance to retry the full request after a terminal error event.
func TestMetaMidStreamServerErrorsReturnRetry(t *testing.T) {
	for _, testCase := range []struct {
		name, errorObject string
		kind              sdkgo.FailureKind
	}{
		{"server_shutting_down", `"type":"server_error","param":null,"code":"server_shutting_down"`, sdkgo.FailureAvailability},
		{"service_overloaded", `"type":"server_error","param":null,"code":"service_overloaded"`, sdkgo.FailureAvailability},
		{"rate_limit_exceeded", `"type":"rate_limit_error","param":null,"code":"rate_limit_exceeded"`, sdkgo.FailureRateLimit},
		{"code-only-server_shutting_down", `"code":"server_shutting_down"`, sdkgo.FailureAvailability},
		{"code-only-service_overloaded", `"code":"service_overloaded"`, sdkgo.FailureAvailability},
		{"code-only-backend_unavailable", `"code":"backend_unavailable"`, sdkgo.FailureAvailability},
		{"code-only-rate_limit_exceeded", `"code":"rate_limit_exceeded"`, sdkgo.FailureRateLimit},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := textgentest.NewFakeProvider(t, metaDialect.CredentialHeader, metaTestAPIKey)
			provider.EnqueueReplies(textgentest.FakeReply{
				Header: http.Header{"Content-Type": {"text/event-stream"}},
				Body: `data: {"id":"chatcmpl-meta-2","object":"chat.completion.chunk","model":"muse-spark-1.3",` +
					`"choices":[{"index":0,"delta":{"role":"assistant","content":"Partial"},"finish_reason":null}]}` + "\n\n" +
					fmt.Sprintf(`data: {"error":{"message":"Please retry your request.",%s}}`, testCase.errorObject) + "\n\n",
			})
			_, err := sdkgo.RunQuery(testsupport.NewDexContext("meta-flow", "meta-mid-stream-"+testCase.name),
				newMetaClient(t, provider.BaseURL(), "").GenerateText(), metaTestConnection, metaUserRequest("Hello"))
			var retryError *sdkgo.RetryError
			require.ErrorAs(t, err, &retryError)
			require.Equal(t, testCase.kind, retryError.Failure.Kind)
			require.NotContains(t, retryError.Failure.Message, "Please retry", "a Failure never carries provider message text")
		})
	}
}

// TestMetaNotImplementedIsAConclusiveRejection keeps 501 out of the server_error retry rule.
func TestMetaNotImplementedIsAConclusiveRejection(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, metaDialect.CredentialHeader, metaTestAPIKey)
	provider.EnqueueReplies(textgentest.FakeReply{
		StatusCode: http.StatusNotImplemented, Header: http.Header{"Content-Type": {"application/json"}},
		Body: `{"error":{"message":"Not implemented.","type":"server_error","param":null,"code":null}}`,
	})
	result := runMetaGenerateText(t, newMetaClient(t, provider.BaseURL(), ""), metaUserRequest("Hello"))
	require.Equal(t, llm.GenerateTextBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureProviderRejection, result.Failure.Kind)
}

func newMetaQuery(t testing.TB, connection textgentest.FakeConnection) *textgen.TextGenerationQuery {
	return newMetaFakeProviderClient(t, connection).GenerateText()
}

// newMetaFakeProviderClient builds a Client for a textgentest suite's fake provider and connection.
func newMetaFakeProviderClient(t testing.TB, connection textgentest.FakeConnection) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderMeta, Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[llm.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		llm.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newMetaClient(t *testing.T, baseURL string, model string) *llm.Client {
	t.Helper()
	client, err := llm.New(llm.Config{Provider: llm.ProviderMeta, Model: model}, metaTestCredentials(), llm.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func metaTestCredentials() sdkgo.StaticCredentialProvider[llm.Credentials] {
	return sdkgo.StaticCredentialProvider[llm.Credentials]{metaTestConnection: {APIKey: sdkgo.NewSecretString(metaTestAPIKey)}}
}

func runMetaGenerateText(t *testing.T, client *llm.Client, request llm.GenerateTextRequest) llm.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("meta-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("meta-flow", stepExecutionID), client.GenerateText(), metaTestConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), metaTestAPIKey), "a Result contains the API key")
	return result
}

func metaUserRequest(text string) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: text}}}
}
