// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meta_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/meta"
	"github.com/superdurable/dex-connectors-library/connectors/meta/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/openaichat/openaichattest"
)

const testAPIKey = "LLM|1234567890|meta-connector-test-key"

var testConnection = sdkgo.ConnectionRef{Provider: "meta", Name: "meta-test"}

// metaDialect matches chatProfile: streamed replies, the default 402 quota error, and Meta's 400 content-policy code.
var metaDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "muse-spark-1.3", AlternateModel: "muse-spark-1.2", IsStreaming: true,
	ContentPolicyErrorToken: "content_policy_violation",
})

func TestGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange := 2.1
	llmtest.RunTextGenerationExchangeSuite(t, &llmtest.TextGenerationExchangeSuite{
		Dialect:  metaDialect,
		NewQuery: newMetaQuery,
		LocallyRejectedRequests: []llmtest.NamedTextGenerationRequest{
			{Name: "temperature above 2", Request: llm.TextGenerationRequest{Temperature: &temperatureAboveRange}},
			{Name: "reasoning effort none, which Muse Spark rejects", Request: llm.TextGenerationRequest{
				ReasoningEffort: llm.ReasoningEffortNone,
			}},
			{Name: "max reasoning effort on muse-spark-1.2", Request: llm.TextGenerationRequest{
				Model: "muse-spark-1.2", ReasoningEffort: llm.ReasoningEffortMax,
			}},
			{Name: "max reasoning effort on the Contributor tier", Request: llm.TextGenerationRequest{
				Model: "muse-spark-1.3-contributor", ReasoningEffort: llm.ReasoningEffortMax,
			}},
		},
	})
}

// TestGenerateTextSendsTheDocumentedChatCompletionsRequest pins the body fields Meta documents for Chat Completions.
func TestGenerateTextSendsTheDocumentedChatCompletionsRequest(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, metaDialect.CredentialHeader, testAPIKey)
	reply := metaDialect.GeneratedReply(llmtest.GeneratedReply{
		Text: `{"answer":"TCP is reliable."}`, ServedModel: "muse-spark-1.3", ResponseID: "chatcmpl-meta-1",
		Usage: llm.Usage{InputTokens: 25, CachedInputTokens: 5, OutputTokens: 19, ReasoningTokens: 12, TotalTokens: 44},
	})
	reply.Header.Set("x-ratelimit-remaining-requests", "2999")
	reply.Header.Set("x-ratelimit-remaining-tokens", "3999956")
	provider.EnqueueReplies(reply)
	temperature := 0.4
	result := runGenerateText(t, newMetaClient(t, provider.BaseURL(), ""), meta.GenerateTextRequest{
		Instructions: "Answer in one sentence.",
		Messages:     []llm.Message{{Role: llm.MessageRoleUser, Text: "How does TCP differ from UDP?"}},
		StructuredOutput: &llm.StructuredOutput{Name: "answer", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
			"required": []any{"answer"}, "additionalProperties": false,
		}},
		MaxOutputTokens: 2048, Temperature: &temperature, ReasoningEffort: llm.ReasoningEffortMax,
	})

	require.Equal(t, meta.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
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

// TestGenerateTextMapsExtraHighReasoningToXHigh keeps the one effort whose wire value differs from its name.
func TestGenerateTextMapsExtraHighReasoningToXHigh(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, metaDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(metaDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Done.", ServedModel: "muse-spark-1.2"}))
	result := runGenerateText(t, newMetaClient(t, provider.BaseURL(), "muse-spark-1.2"), meta.GenerateTextRequest{
		Messages:        []llm.Message{{Role: llm.MessageRoleUser, Text: "Plan the migration."}},
		ReasoningEffort: llm.ReasoningEffortExtraHigh,
	})
	require.Equal(t, meta.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	var body struct {
		ReasoningEffort string `json:"reasoning_effort"`
	}
	require.NoError(t, json.Unmarshal(provider.Requests()[0].Body, &body))
	require.Equal(t, "xhigh", body.ReasoningEffort)
}

// TestMidStreamServerErrorsReturnRetry follows Meta's guidance to retry the full request after a terminal error event.
func TestMidStreamServerErrorsReturnRetry(t *testing.T) {
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
			provider := llmtest.NewFakeProvider(t, metaDialect.CredentialHeader, testAPIKey)
			provider.EnqueueReplies(llmtest.FakeReply{
				Header: http.Header{"Content-Type": {"text/event-stream"}},
				Body: `data: {"id":"chatcmpl-meta-2","object":"chat.completion.chunk","model":"muse-spark-1.3",` +
					`"choices":[{"index":0,"delta":{"role":"assistant","content":"Partial"},"finish_reason":null}]}` + "\n\n" +
					fmt.Sprintf(`data: {"error":{"message":"Please retry your request.",%s}}`, testCase.errorObject) + "\n\n",
			})
			_, err := sdkgo.RunQuery(testsupport.NewDexContext("meta-flow", "meta-mid-stream-"+testCase.name),
				newMetaClient(t, provider.BaseURL(), "").GenerateText(), testConnection, userRequest("Hello"))
			var retryError *sdkgo.RetryError
			require.ErrorAs(t, err, &retryError)
			require.Equal(t, testCase.kind, retryError.Failure.Kind)
			require.NotContains(t, retryError.Failure.Message, "Please retry", "a Failure never carries provider message text")
		})
	}
}

// TestNotImplementedIsAConclusiveRejection keeps 501 out of the server_error retry rule.
func TestNotImplementedIsAConclusiveRejection(t *testing.T) {
	provider := llmtest.NewFakeProvider(t, metaDialect.CredentialHeader, testAPIKey)
	provider.EnqueueReplies(llmtest.FakeReply{
		StatusCode: http.StatusNotImplemented, Header: http.Header{"Content-Type": {"application/json"}},
		Body: `{"error":{"message":"Not implemented.","type":"server_error","param":null,"code":null}}`,
	})
	result := runGenerateText(t, newMetaClient(t, provider.BaseURL(), ""), userRequest("Hello"))
	require.Equal(t, meta.GenerateTextBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureProviderRejection, result.Failure.Kind)
}

// TestGenerateTextCallsTheMetaModelAPIHost proves the production URL without a network call.
func TestGenerateTextCallsTheMetaModelAPIHost(t *testing.T) {
	reply := metaDialect.GeneratedReply(llmtest.GeneratedReply{Text: "Hello.", ServedModel: "muse-spark-1.3"})
	var requestedURLs []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestedURLs = append(requestedURLs, request.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK, Header: reply.Header.Clone(), Body: io.NopCloser(strings.NewReader(reply.Body)), Request: request,
		}, nil
	})
	client, err := meta.New(meta.Config{}, testCredentials(), meta.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	result := runGenerateText(t, client, userRequest("Hello"))
	require.Equal(t, meta.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "muse-spark-1.3", result.Value.RequestedModel, "a blank connection model uses the manifest default")
	require.Equal(t, []string{"https://api.meta.ai/v1/chat/completions"}, requestedURLs)
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	for _, baseURL := range []string{"https://api.meta.ai.example.com", "http://192.0.2.10:8080", "https://attacker.example"} {
		_, err := meta.New(meta.Config{}, testCredentials(), meta.WithBaseURLForTest(baseURL))
		require.ErrorContains(t, err, "loopback", baseURL)
	}
	for _, baseURL := range []string{"http://127.0.0.1:8080", "http://localhost:9000", "http://[::1]:7000"} {
		_, err := meta.New(meta.Config{}, testCredentials(), meta.WithBaseURLForTest(baseURL))
		require.NoError(t, err, baseURL)
	}
	_, err := meta.New(meta.Config{}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = meta.New(meta.Config{Model: "muse-spark-é"}, testCredentials())
	require.Error(t, err, "an invalid connection model fails at startup")
	_, err = meta.New(meta.Config{MaxResponseBytes: -1}, testCredentials())
	require.ErrorContains(t, err, "maxResponseBytes")
	_, err = meta.New(meta.Config{}, testCredentials(), nil)
	require.ErrorContains(t, err, "option is nil")
}

func newMetaQuery(t testing.TB, connection llmtest.FakeConnection) *llm.TextGenerationQuery {
	return newFakeProviderClient(t, connection).GenerateText()
}

// newFakeProviderClient builds a Client for an llmtest suite's fake provider and connection.
func newFakeProviderClient(t testing.TB, connection llmtest.FakeConnection) *meta.Client {
	t.Helper()
	client, err := meta.New(meta.Config{Model: connection.Model, MaxResponseBytes: connection.MaxResponseBytes},
		sdkgo.StaticCredentialProvider[meta.Credentials]{connection.Reference: {APIKey: connection.APIKey}},
		meta.WithBaseURLForTest(connection.BaseURL))
	require.NoError(t, err)
	return client
}

func newMetaClient(t *testing.T, baseURL string, model string) *meta.Client {
	t.Helper()
	client, err := meta.New(meta.Config{Model: model}, testCredentials(), meta.WithBaseURLForTest(baseURL))
	require.NoError(t, err)
	return client
}

func testCredentials() sdkgo.StaticCredentialProvider[meta.Credentials] {
	return sdkgo.StaticCredentialProvider[meta.Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
}

func runGenerateText(t *testing.T, client *meta.Client, request meta.GenerateTextRequest) meta.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("meta-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("meta-flow", stepExecutionID), client.GenerateText(), testConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), testAPIKey), "a Result contains the API key")
	return result
}

func userRequest(text string) meta.GenerateTextRequest {
	return meta.GenerateTextRequest{Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: text}}}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
