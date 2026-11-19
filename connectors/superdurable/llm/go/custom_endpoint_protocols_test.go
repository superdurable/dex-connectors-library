// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/go"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/go/internal/messagestest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen"
	"github.com/superdurable/dex-connectors-library/sdkgo/textgen/textgentest"
)

// anthropicCompatibleDialect streams Messages replies for gateway model IDs that are not Claude IDs.
var anthropicCompatibleDialect = messagestest.NewProviderDialect("gateway-model", "gateway-model-2")

func TestAnthropicCompatibleGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveOne := 1.5
	textgentest.RunTextGenerationExchangeSuite(t, &textgentest.TextGenerationExchangeSuite{
		Dialect: anthropicCompatibleDialect,
		NewQuery: func(t testing.TB, connection textgentest.FakeConnection) *textgen.TextGenerationQuery {
			return newCustomEndpointSuiteClient(t, connection, llm.Config{
				Provider: llm.ProviderAnthropicCompatible, BaseURL: connection.BaseURL + "/anthropic",
			}).GenerateText()
		},
		LocallyRejectedRequests: []textgentest.NamedTextGenerationRequest{
			{Name: "temperature above the Messages API maximum of 1", Request: textgen.TextGenerationRequest{Temperature: &temperatureAboveOne}},
			{Name: "reasoning effort, which gateways do not share", Request: textgen.TextGenerationRequest{ReasoningEffort: textgen.ReasoningEffortLow}},
		},
	})
}

// TestAnthropicCompatibleSendsMessagesToTheConfiguredRoot pins the path, headers, and portable body.
func TestAnthropicCompatibleSendsMessagesToTheConfiguredRoot(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, anthropicCompatibleDialect.CredentialHeader, routingTestAPIKey)
	provider.EnqueueReplies(messagestest.StreamReply(textgentest.GeneratedReply{
		Text: `{"answer":"yes"}`, ServedModel: "gateway-model", ResponseID: "msg_gateway_1",
	}, "end_turn"))
	client, err := llm.New(llm.Config{
		Provider: llm.ProviderAnthropicCompatible, BaseURL: provider.BaseURL() + "/anthropic/", Model: " gateway-model ",
	}, routingCredentials())
	require.NoError(t, err)
	temperature := 0.3
	result := runRoutingGenerateText(t, client, llm.GenerateTextRequest{
		Instructions: "Answer briefly.", Temperature: &temperature, MaxOutputTokens: 64,
		Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: "Reply with yes."}},
		StructuredOutput: &textgen.StructuredOutput{Name: "answer", Schema: map[string]any{
			"type": "object", "properties": map[string]any{"answer": map[string]any{"type": "string"}},
			"required": []any{"answer"}, "additionalProperties": false,
		}},
	})
	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "%+v", result.Failure)
	require.Equal(t, `{"answer":"yes"}`, result.Value.Text)
	require.Equal(t, "gateway-model", result.Value.RequestedModel)
	require.Equal(t, "anthropic-compatible", result.Receipt.Provider)
	requests := provider.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].Method)
	require.Equal(t, "/anthropic/v1/messages", requests[0].Path)
	require.Equal(t, "2023-06-01", requests[0].Header.Get("anthropic-version"))
	require.Empty(t, requests[0].Header.Get("anthropic-workspace-id"))
	require.True(t, requests[0].HasCredentialInSlot)
	require.False(t, requests[0].HasCredentialOutsideSlot)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(requests[0].Body, &body))
	require.ElementsMatch(t, []string{"model", "max_tokens", "system", "messages", "temperature", "stream"}, bodyFieldNames(body),
		"a gateway receives no Claude-only output_config")
	require.JSONEq(t, `"gateway-model"`, string(body["model"]))
	require.JSONEq(t, `64`, string(body["max_tokens"]))
	require.JSONEq(t, `0.3`, string(body["temperature"]))
	require.Contains(t, string(body["system"]), "Answer briefly.")
	require.Contains(t, string(body["system"]), `\"answer\"`, "the schema travels in the instructions")
}

func TestAnthropicCompatibleRejectsInvalidConfiguration(t *testing.T) {
	for _, testCase := range []struct {
		name, baseURL, model, errorText string
	}{
		{"missing URL", "", "gateway-model", "base URL is required"},
		{"missing model", "https://example.com/anthropic", "", "model is required for provider anthropic-compatible"},
		{"OpenAI-style /v1 root", "https://example.com/v1", "gateway-model", "must not end in /v1"},
		{"full Messages URL", "https://example.com/anthropic/v1/messages", "gateway-model", "must not end in /v1 or /messages"},
		{"public HTTP", "http://example.com/anthropic", "gateway-model", "must use HTTPS"},
		{"private IPv4", "https://10.0.0.1/anthropic", "gateway-model", "public or loopback"},
		{"metadata IPv4", "https://169.254.169.254", "gateway-model", "public or loopback"},
		{"credential in URL", "https://key@example.com/anthropic", "gateway-model", "user information"},
		{"query in URL", "https://example.com/anthropic?key=SENTINEL", "gateway-model", "query or a fragment"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := llm.New(llm.Config{
				Provider: llm.ProviderAnthropicCompatible, BaseURL: testCase.baseURL, Model: testCase.model,
			}, routingCredentials())
			require.ErrorContains(t, err, testCase.errorText)
			if testCase.baseURL != "" {
				require.NotContains(t, err.Error(), testCase.baseURL)
			}
			require.NotContains(t, err.Error(), "SENTINEL")
		})
	}
	_, err := llm.New(llm.Config{Provider: llm.ProviderAnthropicCompatible, BaseURL: "https://example.com", Model: "gateway-model"},
		routingCredentials())
	require.NoError(t, err, "a host root is a valid Anthropic base URL")
	_, err = llm.New(llm.Config{
		Provider: llm.ProviderAnthropicCompatible, BaseURL: "https://example.com/anthropic", Model: "gateway-model",
		AnthropicWorkspaceID: "wrkspc_01JwQvzr7rXLA5AGx3HKfFUJ",
	}, routingCredentials())
	require.ErrorContains(t, err, "anthropicWorkspaceId applies only to provider anthropic")
	_, err = llm.New(llm.Config{
		Provider: llm.ProviderAnthropicCompatible, BaseURL: "https://example.com/anthropic", Model: "gateway-model",
		OpenAICompatibleAPI: llm.OpenAICompatibleAPIResponses,
	}, routingCredentials())
	require.ErrorContains(t, err, "openaiCompatibleApi applies only to provider openai-compatible")
	_, err = llm.New(llm.Config{Provider: llm.ProviderAnthropic, BaseURL: "https://example.com/anthropic"}, routingCredentials())
	require.ErrorContains(t, err, "baseUrl applies only to provider openai-compatible or anthropic-compatible")
	_, err = llm.New(llm.Config{Provider: llm.ProviderAnthropicCompatible, BaseURL: "https://example.com/anthropic", Model: "gateway-model"},
		routingCredentials(), llm.WithHTTPClient(&http.Client{Transport: &productionRequestRecorder{}}))
	require.ErrorContains(t, err, "transport must be nil or an *http.Transport")
}

// TestAnthropicCompatibleDoesNotFollowRedirects keeps the bearer key on the configured gateway host.
func TestAnthropicCompatibleDoesNotFollowRedirects(t *testing.T) {
	redirectTarget := textgentest.NewFakeProvider(t, anthropicCompatibleDialect.CredentialHeader, routingTestAPIKey)
	provider := textgentest.NewFakeProvider(t, anthropicCompatibleDialect.CredentialHeader, routingTestAPIKey)
	provider.EnqueueReplies(textgentest.FakeReply{
		StatusCode: http.StatusTemporaryRedirect, Header: http.Header{"Location": {redirectTarget.BaseURL() + "/v1/messages"}},
	})
	client, err := llm.New(llm.Config{
		Provider: llm.ProviderAnthropicCompatible, BaseURL: provider.BaseURL(), Model: "gateway-model",
	}, routingCredentials())
	require.NoError(t, err)
	_ = runRoutingGenerateText(t, client, routingUserRequest(""))
	require.Len(t, provider.Requests(), 1)
	require.Empty(t, redirectTarget.Requests(), "a redirect cannot forward the API key")
}

func TestOpenAICompatibleResponsesGenerateTextFollowsTheExchangeContract(t *testing.T) {
	temperatureAboveRange := 2.1
	textgentest.RunTextGenerationExchangeSuite(t, &textgentest.TextGenerationExchangeSuite{
		Dialect: responsesDialect,
		NewQuery: func(t testing.TB, connection textgentest.FakeConnection) *textgen.TextGenerationQuery {
			return newCustomEndpointSuiteClient(t, connection, llm.Config{
				Provider: llm.ProviderOpenaiCompatible, BaseURL: connection.BaseURL + "/v1",
				OpenAICompatibleAPI: llm.OpenAICompatibleAPIResponses,
			}).GenerateText()
		},
		LocallyRejectedRequests: []textgentest.NamedTextGenerationRequest{
			{Name: "temperature above 2", Request: textgen.TextGenerationRequest{Temperature: &temperatureAboveRange}},
		},
	})
}

// TestOpenAICompatibleResponsesCallsTheConfiguredRoot pins /responses under the /v1 root and the stateless body.
func TestOpenAICompatibleResponsesCallsTheConfiguredRoot(t *testing.T) {
	provider := textgentest.NewFakeProvider(t, responsesDialect.CredentialHeader, routingTestAPIKey)
	provider.EnqueueReplies(responsesDialect.GeneratedReply(textgentest.GeneratedReply{
		Text: "Gateway reply.", ServedModel: "gateway-model", ResponseID: "resp_gateway_1",
	}))
	client, err := llm.New(llm.Config{
		Provider: llm.ProviderOpenaiCompatible, BaseURL: provider.BaseURL() + "/v1", Model: "gateway-model",
		OpenAICompatibleAPI: llm.OpenAICompatibleAPIResponses,
	}, routingCredentials())
	require.NoError(t, err)
	result := runRoutingGenerateText(t, client, routingUserRequest(""))
	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "%+v", result.Failure)
	require.Equal(t, "Gateway reply.", result.Value.Text)
	require.Equal(t, "openai-compatible", result.Receipt.Provider)
	requests := provider.Requests()
	require.Len(t, requests, 1)
	require.Equal(t, "/v1/responses", requests[0].Path)
	require.True(t, requests[0].HasCredentialInSlot)
	var body map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(requests[0].Body, &body))
	require.JSONEq(t, `false`, string(body["store"]), "a gateway never stores the Response")
	require.JSONEq(t, `"gateway-model"`, string(body["model"]))
}

// TestOpenAICompatibleAPIDefaultsToChatCompletions keeps connections saved before the field on Chat Completions.
func TestOpenAICompatibleAPIDefaultsToChatCompletions(t *testing.T) {
	for _, api := range []llm.OpenAICompatibleAPI{"", llm.OpenAICompatibleAPIChatCompletions} {
		provider := textgentest.NewFakeProvider(t, compatibleDialect.CredentialHeader, routingTestAPIKey)
		provider.EnqueueReplies(compatibleDialect.GeneratedReply(textgentest.GeneratedReply{Text: "Chat reply.", ServedModel: "qwen-example"}))
		client, err := llm.New(llm.Config{
			Provider: llm.ProviderOpenaiCompatible, BaseURL: provider.BaseURL() + "/v1", Model: "qwen-example", OpenAICompatibleAPI: api,
		}, routingCredentials())
		require.NoError(t, err)
		result := runRoutingGenerateText(t, client, routingUserRequest(""))
		require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "%+v", result.Failure)
		require.Len(t, provider.Requests(), 1)
		require.Equal(t, "/v1/chat/completions", provider.Requests()[0].Path, "api %q", api)
	}
	_, err := llm.New(llm.Config{Provider: llm.ProviderOpenai, OpenAICompatibleAPI: llm.OpenAICompatibleAPIResponses}, routingCredentials())
	require.ErrorContains(t, err, "openaiCompatibleApi applies only to provider openai-compatible")
	_, err = llm.New(llm.Config{Provider: llm.ProviderOpenai, OpenAICompatibleAPI: "assistants"}, routingCredentials())
	require.ErrorContains(t, err, "openaiCompatibleApi is invalid")
}

func TestConnectionModelListValidation(t *testing.T) {
	tooMany := make([]string, 101)
	for index := range tooMany {
		tooMany[index] = "model-" + string(rune('a'+index%26)) + string(rune('a'+index/26))
	}
	for _, testCase := range []struct {
		name            string
		config          llm.Config
		errorText       string
		hiddenModelText string
	}{
		{"fixed provider default listed", llm.Config{Provider: llm.ProviderOpenai, Models: []string{"gpt-6-luna", "gpt-6-sol"}}, "", ""},
		{"no list", llm.Config{Provider: llm.ProviderOpenai, Models: []string{}, ModelContextWindowTokens: []string{}}, "", ""},
		{"custom endpoint with windows", llm.Config{
			Provider: llm.ProviderAnthropicCompatible, BaseURL: "https://example.com/anthropic", Model: "gateway-model",
			Models: []string{" gateway-model ", "gateway=model-2"}, ModelContextWindowTokens: []string{"gateway-model=128000", "gateway=model-2= 100000000 "},
		}, "", ""},
		{"blank entry", llm.Config{Provider: llm.ProviderOpenai, Models: []string{"gpt-6-sol", " "}}, "models entry 2", ""},
		{"entry with a space", llm.Config{Provider: llm.ProviderOpenai, Models: []string{"gpt-6-sol", "secret model"}}, "models entry 2", "secret model"},
		{"duplicate after trimming", llm.Config{Provider: llm.ProviderOpenai, Models: []string{"gpt-6-sol", " gpt-6-sol"}}, "entry 2 repeats", ""},
		{"too many", llm.Config{Provider: llm.ProviderOpenai, Models: tooMany}, "more than 100 entries", ""},
		{"default model not listed", llm.Config{Provider: llm.ProviderOpenai, Models: []string{"gpt-6-luna"}}, "must be one of the models entries", "gpt-6-luna"},
		{"configured model not listed", llm.Config{Provider: llm.ProviderOpenai, Model: "gpt-6-astra", Models: []string{"gpt-6-sol"}}, "must be one of the models entries", "gpt-6-astra"},
		{"window without a list", llm.Config{Provider: llm.ProviderOpenai, ModelContextWindowTokens: []string{"gpt-6-sol=1000"}}, "entry 1 names a model that models does not list", ""},
		{"window without separator", llm.Config{Provider: llm.ProviderOpenai, Models: []string{"gpt-6-sol"}, ModelContextWindowTokens: []string{"gpt-6-sol"}}, "<model ID>=<tokens>", ""},
		{"zero window", llm.Config{Provider: llm.ProviderOpenai, Models: []string{"gpt-6-sol"}, ModelContextWindowTokens: []string{"gpt-6-sol=0"}}, "from 1 to 100000000", ""},
		{"window above the bound", llm.Config{Provider: llm.ProviderOpenai, Models: []string{"gpt-6-sol"}, ModelContextWindowTokens: []string{"gpt-6-sol=100000001"}}, "from 1 to 100000000", ""},
		{"negative window", llm.Config{Provider: llm.ProviderOpenai, Models: []string{"gpt-6-sol"}, ModelContextWindowTokens: []string{"gpt-6-sol=-5"}}, "from 1 to 100000000", ""},
		{"fractional window", llm.Config{Provider: llm.ProviderOpenai, Models: []string{"gpt-6-sol"}, ModelContextWindowTokens: []string{"gpt-6-sol=1.5"}}, "from 1 to 100000000", ""},
		{"repeated window", llm.Config{Provider: llm.ProviderOpenai, Models: []string{"gpt-6-sol"}, ModelContextWindowTokens: []string{"gpt-6-sol=1000", "gpt-6-sol=2000"}}, "entry 2 repeats", ""},
		{"gemini path rule", llm.Config{Provider: llm.ProviderGemini, Models: []string{"models/gemini-3.5-flash-lite", "gemini-3.5-pro"}, ModelContextWindowTokens: []string{"gemini-3.5-pro=1048576"}}, "", ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := llm.New(testCase.config, routingCredentials())
			if testCase.errorText == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, testCase.errorText)
			if testCase.hiddenModelText != "" {
				require.NotContains(t, err.Error(), testCase.hiddenModelText, "errors never repeat a configured model")
			}
		})
	}
}

// newCustomEndpointSuiteClient points a custom-endpoint config at a textgentest suite's fake provider.
func newCustomEndpointSuiteClient(t testing.TB, connection textgentest.FakeConnection, config llm.Config) *llm.Client {
	t.Helper()
	config.Model, config.MaxResponseBytes = connection.Model, connection.MaxResponseBytes
	client, err := llm.New(config, sdkgo.StaticCredentialProvider[llm.Credentials]{connection.Reference: {APIKey: connection.APIKey}})
	require.NoError(t, err)
	return client
}
