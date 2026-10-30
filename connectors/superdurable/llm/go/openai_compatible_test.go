// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/go"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/openaichat/openaichattest"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
)

var compatibleDialect = openaichattest.NewProviderDialect(&openaichattest.ProviderDialectConfig{
	ConnectionModel: "qwen-example", AlternateModel: "qwen-example-2", IsStreaming: true,
})

// TestOpenAICompatibleCallsConfiguredLoopbackEndpoint proves the configured /v1 root and shared streamed decoder.
func TestOpenAICompatibleCallsConfiguredLoopbackEndpoint(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, compatibleDialect.CredentialHeader, routingTestAPIKey)
	provider.EnqueueReplies(compatibleDialect.GeneratedReply(textgentest.GeneratedReply{
		Text: "Gateway reply.", ServedModel: "qwen-example", ResponseID: "chatcmpl-compatible-1",
	}))
	client, err := llm.New(llm.Config{
		Provider: llm.ProviderOpenaiCompatible, BaseURL: provider.BaseURL() + "/v1", Model: "qwen-example",
	}, routingCredentials())
	require.NoError(t, err)
	result := runRoutingGenerateText(t, client, routingUserRequest(""))
	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "%+v", result.Failure)
	require.Equal(t, "Gateway reply.", result.Value.Text)
	require.Equal(t, "qwen-example", result.Value.RequestedModel)
	require.Equal(t, "openai-compatible", result.Receipt.Provider)
	requests := provider.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, "/v1/chat/completions", requests[0].Path)
	require.Equal(t, http.MethodPost, requests[0].Method)
	require.True(t, requests[0].HasCredentialInSlot)
	require.False(t, requests[0].HasCredentialOutsideSlot)
	model, err := compatibleDialect.ReadRequestModel(requests[0])
	require.NoError(t, err)
	require.Equal(t, "qwen-example", model)
}

// TestOpenAICompatibleStructuredOutputReusesChatCodec checks JSON instruction and response validation on the custom URL.
func TestOpenAICompatibleStructuredOutputReusesChatCodec(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, compatibleDialect.CredentialHeader, routingTestAPIKey)
	provider.EnqueueReplies(compatibleDialect.GeneratedReply(textgentest.GeneratedReply{
		Text: `{"answer":"yes"}`, ServedModel: "qwen-example",
	}))
	client, err := llm.New(llm.Config{
		Provider: llm.ProviderOpenaiCompatible, BaseURL: provider.BaseURL() + "/v1", Model: "qwen-example",
	}, routingCredentials())
	require.NoError(t, err)
	result := runRoutingGenerateText(t, client, llm.GenerateTextRequest{
		Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: "Reply with yes."}},
		StructuredOutput: &textgen.StructuredOutput{Name: "answer", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
			"required": []any{"answer"}, "additionalProperties": false,
		}},
	})
	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "%+v", result.Failure)
	require.Equal(t, `{"answer":"yes"}`, result.Value.Text)
	require.Len(t, provider.Requests(), 1)
	require.Contains(t, string(provider.Requests()[0].Body), `"response_format":{"type":"json_object"}`)
	require.Contains(t, string(provider.Requests()[0].Body), `\"answer\"`)
}

// TestOpenAICompatibleRejectsInvalidConfiguration keeps unsafe URLs and incomplete gateway settings out of startup.
func TestOpenAICompatibleRejectsInvalidConfiguration(t *testing.T) {
	for _, testCase := range []struct {
		name, baseURL, model, errorText string
	}{
		{"missing URL", "", "qwen-example", "base URL is required"},
		{"missing model", "https://example.com/v1", "", "model is required"},
		{"wrong API root", "https://example.com/api", "qwen-example", "end in /v1"},
		{"public HTTP", "http://example.com/v1", "qwen-example", "must use HTTPS"},
		{"private IPv4", "https://10.0.0.1/v1", "qwen-example", "public or loopback"},
		{"private IPv6", "https://[fd00::1]/v1", "qwen-example", "public or loopback"},
		{"metadata IPv4", "https://169.254.169.254/v1", "qwen-example", "public or loopback"},
		{"metadata IPv6", "https://[fd00:ec2::254]/v1", "qwen-example", "public or loopback"},
		{"credential in URL", "https://key@example.com/v1", "qwen-example", "user information"},
		{"query in URL", "https://example.com/v1?key=SENTINEL", "qwen-example", "query or a fragment"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := llm.New(llm.Config{
				Provider: llm.ProviderOpenaiCompatible, BaseURL: testCase.baseURL, Model: testCase.model,
			}, routingCredentials())
			require.ErrorContains(t, err, testCase.errorText)
			if testCase.baseURL != "" {
				require.NotContains(t, err.Error(), testCase.baseURL)
			}
			require.NotContains(t, err.Error(), "SENTINEL")
		})
	}
	_, err := llm.New(llm.Config{Provider: llm.ProviderOpenai, BaseURL: "https://example.com/v1"}, routingCredentials())
	require.ErrorContains(t, err, "baseUrl applies only to provider openai-compatible")
	_, err = llm.New(llm.Config{Provider: llm.ProviderOpenai, BaseURL: " "}, routingCredentials())
	require.ErrorContains(t, err, "baseUrl applies only to provider openai-compatible")

	_, err = llm.New(llm.Config{Provider: llm.ProviderOpenaiCompatible, BaseURL: "https://example.com/v1", Model: "qwen-example"},
		routingCredentials())
	require.NoError(t, err, "a public HTTPS URL is valid without a startup network call")
	_, err = llm.New(llm.Config{Provider: llm.ProviderOpenaiCompatible, BaseURL: "http://LOCALHOST:8080/v1", Model: "qwen-example"},
		routingCredentials())
	require.NoError(t, err, "loopback HTTP is case-insensitive")
	_, err = llm.New(llm.Config{Provider: llm.ProviderOpenaiCompatible, BaseURL: "https://example.com/v1", Model: "qwen-example"},
		routingCredentials(), llm.WithHTTPClient(&http.Client{Transport: &productionRequestRecorder{}}))
	require.ErrorContains(t, err, "transport must be nil or an *http.Transport")
}

// TestOpenAICompatibleClassifiesProviderErrors uses the shared Chat Completions error path.
func TestOpenAICompatibleClassifiesProviderErrors(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, compatibleDialect.CredentialHeader, routingTestAPIKey)
	provider.EnqueueReplies(compatibleDialect.ErrorReply(http.StatusUnauthorized, "SENTINEL secret error text"))
	client, err := llm.New(llm.Config{
		Provider: llm.ProviderOpenaiCompatible, BaseURL: provider.BaseURL() + "/v1", Model: "qwen-example",
	}, routingCredentials())
	require.NoError(t, err)
	result := runRoutingGenerateText(t, client, textgen.TextGenerationRequest{
		Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: "Hello"}},
	})
	require.Equal(t, llm.GenerateTextBranchProviderRejected, result.Branch)
	require.NotContains(t, result.Failure.Message, "SENTINEL")
}

// TestOpenAICompatibleDoesNotFollowRedirects keeps the bearer key on the configured gateway host.
func TestOpenAICompatibleDoesNotFollowRedirects(t *testing.T) {
	redirectTarget := textgentest.NewFakeProvider(t, compatibleDialect.CredentialHeader, routingTestAPIKey)
	provider := textgentest.NewFakeProvider(t, compatibleDialect.CredentialHeader, routingTestAPIKey)
	provider.EnqueueReplies(textgentest.FakeReply{
		StatusCode: http.StatusFound, Header: http.Header{"Location": {redirectTarget.BaseURL() + "/v1/chat/completions"}},
	})
	client, err := llm.New(llm.Config{
		Provider: llm.ProviderOpenaiCompatible, BaseURL: provider.BaseURL() + "/v1", Model: "qwen-example",
	}, routingCredentials())
	require.NoError(t, err)
	_ = runRoutingGenerateText(t, client, routingUserRequest(""))
	require.Len(t, provider.Requests(), 1)
	require.True(t, provider.Requests()[0].HasCredentialInSlot)
	require.Empty(t, redirectTarget.Requests(), "a redirect cannot forward the API key")
}
