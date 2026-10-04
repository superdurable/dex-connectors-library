// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm_test

import (
	"encoding/json"
	"fmt"
	"io"
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

const routingTestAPIKey = "sk-SENTINEL-llm-routing-key-0123456789"

var routingTestConnection = sdkgo.ConnectionRef{Provider: "llm", Name: "llm-routing-test"}

// allRegions lists every value of the manifest's region enum.
var allRegions = []llm.Region{llm.RegionGlobal, llm.RegionUs, llm.RegionEu, llm.RegionChina, llm.RegionHongKong}

// servedProvider is one provider the llm connector serves, as its production API receives a call.
type servedProvider struct {
	provider     llm.Provider
	defaultModel string
	dialect      textgentest.ProviderDialect
	// regionURLs maps every region the provider serves to the URL that a default-model call reaches.
	regionURLs map[llm.Region]string
}

// servedProviders lists the provider enum in manifest order.
var servedProviders = []servedProvider{
	{llm.ProviderOpenai, "gpt-6-sol", responsesDialect, map[llm.Region]string{
		llm.RegionGlobal: "https://api.openai.com/v1/responses",
	}},
	{llm.ProviderAnthropic, "claude-sonnet-5", claudeDialect, map[llm.Region]string{
		llm.RegionGlobal: "https://api.anthropic.com/v1/messages",
	}},
	{llm.ProviderGemini, "gemini-3.5-flash-lite", geminiDialect, map[llm.Region]string{
		llm.RegionGlobal: "https://generativelanguage.googleapis.com/v1beta/models/gemini-3.5-flash-lite:generateContent",
	}},
	{llm.ProviderQwen, "qwen3.7-plus", qwenDialect, map[llm.Region]string{
		llm.RegionGlobal:   "https://dashscope-intl.aliyuncs.com/compatible-mode/v1/chat/completions",
		llm.RegionHongKong: "https://cn-hongkong.dashscope.aliyuncs.com/compatible-mode/v1/chat/completions",
		llm.RegionChina:    "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions",
	}},
	{llm.ProviderDeepseek, "deepseek-flash", deepSeekDialect, map[llm.Region]string{
		llm.RegionGlobal: "https://api.deepseek.com/chat/completions",
	}},
	{llm.ProviderMeta, "muse-spark-1.3", metaDialect, map[llm.Region]string{
		llm.RegionGlobal: "https://api.meta.ai/v1/chat/completions",
	}},
	{llm.ProviderMistral, "mistral-large-2512", mistralDialect, map[llm.Region]string{
		llm.RegionGlobal: "https://api.mistral.ai/v1/chat/completions",
		llm.RegionEu:     "https://api.eu.mistral.ai/v1/chat/completions",
		llm.RegionUs:     "https://api.us.mistral.ai/v1/chat/completions",
	}},
	{llm.ProviderKimi, "kimi-k2.6", kimiDialect, map[llm.Region]string{
		llm.RegionGlobal: "https://api.moonshot.ai/v1/chat/completions",
		llm.RegionChina:  "https://api.moonshot.cn/v1/chat/completions",
	}},
	{llm.ProviderXai, "grok-4.3", xaiDialect, map[llm.Region]string{
		llm.RegionGlobal: "https://api.x.ai/v1/chat/completions",
		llm.RegionUs:     "https://us.api.x.ai/v1/chat/completions",
	}},
}

// TestEachProviderCallsItsOwnAPIInEveryRegionItServes proves every production URL, key header, and default model without a network call.
func TestEachProviderCallsItsOwnAPIInEveryRegionItServes(t *testing.T) {
	for _, served := range servedProviders {
		for _, region := range append([]llm.Region{""}, allRegions...) {
			t.Run(fmt.Sprintf("%s in region %q", served.provider, region), func(t *testing.T) {
				expectedURL, isServed := served.regionURLs[region]
				if region == "" {
					expectedURL, isServed = served.regionURLs[llm.RegionGlobal], true
				}
				recorder := &productionRequestRecorder{reply: served.dialect.GeneratedReply(textgentest.GeneratedReply{
					Text: "Hello.", ServedModel: served.defaultModel, ResponseID: "resp-routing",
				})}
				client, err := llm.New(llm.Config{Provider: served.provider, Region: region, Model: " \t "}, routingCredentials(),
					llm.WithHTTPClient(&http.Client{Transport: recorder}))
				if !isServed {
					require.ErrorContains(t, err, fmt.Sprintf("provider %s does not serve region %s; use ", served.provider, region))
					return
				}
				require.NoError(t, err)
				result := runRoutingGenerateText(t, client, routingUserRequest(""))
				require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
				require.Equal(t, "Hello.", result.Value.Text)
				require.Equal(t, served.defaultModel, result.Value.RequestedModel, "a blank connection model uses the provider's default")
				require.Equal(t, string(served.provider), result.Receipt.Provider, "Receipt.Provider names the configured provider")
				require.Len(t, recorder.requests, 1)
				request := recorder.requests[0]
				require.Equal(t, expectedURL, request.URL.String())
				require.Equal(t, http.MethodPost, request.Method)
				if served.provider == llm.ProviderGemini {
					require.Equal(t, routingTestAPIKey, request.Header.Get("x-goog-api-key"))
					require.Empty(t, request.Header.Values("Authorization"))
				} else {
					require.Equal(t, "Bearer "+routingTestAPIKey, request.Header.Get("Authorization"))
					require.Empty(t, request.Header.Values("x-goog-api-key"))
				}
				require.NotContains(t, request.URL.String(), routingTestAPIKey, "the key never travels in the URL")
			})
		}
	}
}

// TestRequestAndConnectionModelsTakePrecedenceOverTheProviderDefault keeps generateText model precedence on every provider.
func TestRequestAndConnectionModelsTakePrecedenceOverTheProviderDefault(t *testing.T) {
	for _, served := range servedProviders {
		t.Run(string(served.provider), func(t *testing.T) {
			connectionModel, requestModel := served.dialect.ConnectionModel, served.dialect.AlternateModel
			for _, testCase := range []struct {
				name, connectionModel, requestModel, sentModel string
			}{
				{"the connection model applies without a request model", connectionModel, "", connectionModel},
				{"a request model overrides the connection model", connectionModel, " " + requestModel + " ", requestModel},
				{"a request model overrides the provider default", "", requestModel, requestModel},
			} {
				provider := textgentest.NewFakeProvider(t, served.dialect.CredentialHeader, routingTestAPIKey)
				provider.EnqueueReplies(served.dialect.GeneratedReply(textgentest.GeneratedReply{Text: "Done.", ServedModel: testCase.sentModel}))
				client, err := llm.New(llm.Config{Provider: served.provider, Model: testCase.connectionModel}, routingCredentials(),
					llm.WithBaseURLForTest(provider.BaseURL()))
				require.NoError(t, err, testCase.name)
				result := runRoutingGenerateText(t, client, routingUserRequest(testCase.requestModel))
				require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "%s: %+v", testCase.name, result.Failure)
				require.Equal(t, testCase.sentModel, result.Value.RequestedModel, testCase.name)
				requests := provider.Requests()
				require.Len(t, requests, 1, testCase.name)
				sentModel, err := served.dialect.ReadRequestModel(requests[0])
				require.NoError(t, err)
				require.Equal(t, testCase.sentModel, sentModel, testCase.name)
			}
		})
	}
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		config        llm.Config
		expectedError string
	}{
		{"a missing provider", llm.Config{}, "configuration provider is required"},
		{"an unknown provider", llm.Config{Provider: "llama"}, "configuration provider is invalid"},
		{"an unknown region", llm.Config{Provider: llm.ProviderOpenai, Region: "mars"}, "configuration region is invalid"},
		{"a region the provider does not serve", llm.Config{Provider: llm.ProviderOpenai, Region: llm.RegionEu},
			"provider openai does not serve region eu; use global"},
		{"a qwen region list", llm.Config{Provider: llm.ProviderQwen, Region: llm.RegionUs},
			"provider qwen does not serve region us; use china or global or hong-kong"},
		{"a workspace on another provider", llm.Config{Provider: llm.ProviderOpenai, AnthropicWorkspaceID: "wrkspc_01JwQvzr7rXLA5AGx3HKfFUJ"},
			"anthropicWorkspaceId applies only to provider anthropic"},
		{"a malformed workspace", llm.Config{Provider: llm.ProviderAnthropic, AnthropicWorkspaceID: "workspace-SENTINEL"},
			"anthropicWorkspaceId must be a wrkspc_ workspace ID"},
		{"a negative response limit", llm.Config{Provider: llm.ProviderMeta, MaxResponseBytes: -1}, "maxResponseBytes cannot be negative"},
		{"a model with spaces on a body-rule provider", llm.Config{Provider: llm.ProviderDeepseek, Model: "deepseek SENTINEL"}, "model ID"},
		{"a model the Gemini path-segment rule rejects", llm.Config{Provider: llm.ProviderGemini, Model: "gemini/SENTINEL"}, "model ID"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := llm.New(testCase.config, routingCredentials())
			require.ErrorContains(t, err, testCase.expectedError)
			require.NotContains(t, err.Error(), "SENTINEL", "errors never repeat a configured model or workspace ID")
		})
	}
	_, err := llm.New(llm.Config{Provider: llm.ProviderOpenai}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	_, err = llm.New(llm.Config{Provider: llm.ProviderOpenai}, routingCredentials(), nil)
	require.ErrorContains(t, err, "option is nil")
}

func TestBaseURLForTestAcceptsOnlyLoopbackHosts(t *testing.T) {
	for _, served := range servedProviders {
		for _, baseURL := range []string{"https://api.openai.com.example.com", "http://192.0.2.10:8080", "https://attacker.example"} {
			_, err := llm.New(llm.Config{Provider: served.provider}, routingCredentials(), llm.WithBaseURLForTest(baseURL))
			require.ErrorContains(t, err, "loopback", "%s %s", served.provider, baseURL)
		}
		for _, baseURL := range []string{"http://127.0.0.1:8080", "http://localhost:9000", "http://[::1]:7000"} {
			_, err := llm.New(llm.Config{Provider: served.provider}, routingCredentials(), llm.WithBaseURLForTest(baseURL))
			require.NoError(t, err, "%s %s", served.provider, baseURL)
		}
	}
}

// TestHTTPClientTimeoutMustEndBeforeTheExecuteTimeout keeps every exchange inside the 1200-second Execute timeout.
func TestHTTPClientTimeoutMustEndBeforeTheExecuteTimeout(t *testing.T) {
	require.Equal(t, 1200*time.Second, llm.GenerateTextDefinition.StepDefaults.ExecuteMethodTimeout)
	for _, served := range servedProviders {
		_, err := llm.New(llm.Config{Provider: served.provider}, routingCredentials(), llm.WithHTTPClient(&http.Client{Timeout: 1200 * time.Second}))
		require.ErrorContains(t, err, "Execute timeout must exceed the request timeout", served.provider)
		callerClient := &http.Client{Timeout: 1199 * time.Second}
		_, err = llm.New(llm.Config{Provider: served.provider}, routingCredentials(), llm.WithHTTPClient(callerClient))
		require.NoError(t, err, served.provider)
		require.Equal(t, 1199*time.Second, callerClient.Timeout, "the connector never modifies the caller's client")
	}
}

// TestCredentialsAreResolvedForEveryCall applies a replaced key without building a new Client.
func TestCredentialsAreResolvedForEveryCall(t *testing.T) {
	credentials := routingCredentials()
	recorder := &productionRequestRecorder{reply: deepSeekDialect.GeneratedReply(textgentest.GeneratedReply{Text: "Hello.", ServedModel: "deepseek-flash"})}
	client, err := llm.New(llm.Config{Provider: llm.ProviderDeepseek}, credentials, llm.WithHTTPClient(&http.Client{Transport: recorder}))
	require.NoError(t, err)
	runRoutingGenerateText(t, client, routingUserRequest(""))
	credentials[routingTestConnection] = llm.Credentials{APIKey: sdkgo.NewSecretString(routingTestAPIKey + "-replaced")}
	runRoutingGenerateText(t, client, routingUserRequest(""))
	require.Len(t, recorder.requests, 2)
	require.Equal(t, "Bearer "+routingTestAPIKey, recorder.requests[0].Header.Get("Authorization"))
	require.Equal(t, "Bearer "+routingTestAPIKey+"-replaced", recorder.requests[1].Header.Get("Authorization"))
}

// TestStoredCredentialsMustHoldExactlyTheAPIKey rejects a provider router record at call time, without a request.
func TestStoredCredentialsMustHoldExactlyTheAPIKey(t *testing.T) {
	for name, credentials := range map[string]map[string]any{
		"a provider router record with per-provider keys": {"auth_methods": []string{"openai"}, "openai_api_key": "sk-SENTINEL-old-record"},
		"a blank key": {"api_key": ""},
	} {
		t.Run(name, func(t *testing.T) {
			project := testsupport.NewLoadedProject(t, llm.ConnectorID, []testsupport.ProjectConnection{{
				Name: routingTestConnection.Name, Configuration: map[string]any{"provider": "openai"}, Credentials: credentials,
			}}, nil)
			recorder := &productionRequestRecorder{}
			connection, err := llm.NewProjectConnection(project, routingTestConnection.Name, llm.WithHTTPClient(&http.Client{Transport: recorder}))
			require.NoError(t, err, "credentials are read during each call, so the Worker starts")
			result, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-flow", fmt.Sprintf("llm-credentials-%d", time.Now().UnixNano())),
				llm.ConnectionGenerateTextQueryForTest(connection), routingTestConnection, routingUserRequest(""))
			require.NoError(t, err)
			require.Equal(t, llm.GenerateTextBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			require.Equal(t, "openai", result.Failure.Provider)
			require.Empty(t, recorder.requests, "a defect selected before dispatch sends nothing")
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "SENTINEL")
		})
	}
}

// TestProjectConnectionCallsTheConfiguredProvider opens a connection from project storage, as an application does.
func TestProjectConnectionCallsTheConfiguredProvider(t *testing.T) {
	project := testsupport.NewLoadedProject(t, llm.ConnectorID, []testsupport.ProjectConnection{{
		Name:          routingTestConnection.Name,
		Configuration: map[string]any{"provider": "kimi", "model": "kimi-k3", "region": "china"},
		Credentials:   map[string]any{"api_key": routingTestAPIKey},
	}}, nil)
	recorder := &productionRequestRecorder{reply: kimiDialect.GeneratedReply(textgentest.GeneratedReply{Text: "Hello.", ServedModel: "kimi-k3"})}
	connection, err := llm.NewProjectConnection(project, routingTestConnection.Name, llm.WithHTTPClient(&http.Client{Transport: recorder}))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-flow", fmt.Sprintf("llm-project-%d", time.Now().UnixNano())),
		llm.ConnectionGenerateTextQueryForTest(connection), routingTestConnection, routingUserRequest(""))
	require.NoError(t, err)
	require.Equal(t, llm.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "kimi-k3", result.Value.RequestedModel)
	require.Equal(t, "kimi", result.Receipt.Provider)
	require.Len(t, recorder.requests, 1)
	require.Equal(t, "https://api.moonshot.cn/v1/chat/completions", recorder.requests[0].URL.String())
	require.Equal(t, "Bearer "+routingTestAPIKey, recorder.requests[0].Header.Get("Authorization"))
}

// productionRequestRecorder answers every request in process with reply and records it.
type productionRequestRecorder struct {
	reply    textgentest.FakeReply
	requests []*http.Request
}

func (recorder *productionRequestRecorder) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder.requests = append(recorder.requests, request)
	header := recorder.reply.Header.Clone()
	if header == nil {
		header = http.Header{}
	}
	return &http.Response{
		StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(recorder.reply.Body)), Request: request,
	}, nil
}

func routingCredentials() sdkgo.StaticCredentialProvider[llm.Credentials] {
	return sdkgo.StaticCredentialProvider[llm.Credentials]{routingTestConnection: {APIKey: sdkgo.NewSecretString(routingTestAPIKey)}}
}

func runRoutingGenerateText(t *testing.T, client *llm.Client, request llm.GenerateTextRequest) llm.GenerateTextResult {
	t.Helper()
	stepExecutionID := fmt.Sprintf("llm-routing-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-routing-flow", stepExecutionID), client.GenerateText(), routingTestConnection, request)
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.False(t, strings.Contains(string(encoded), routingTestAPIKey), "a Result contains the API key")
	return result
}

func routingUserRequest(model string) llm.GenerateTextRequest {
	return llm.GenerateTextRequest{Model: model, Messages: []textgen.Message{{Role: textgen.MessageRoleUser, Text: "Hello"}}}
}
