// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmrouter_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/providerdialecttest"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm"
	"github.com/superdurable/dex-connectors-library/sdkgo/llm/llmtest"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	openAITestKey    = "sk-proj-llmrouter-openai-key-canary"
	anthropicTestKey = "sk-ant-llmrouter-anthropic-key-canary"
	geminiTestKey    = "AIzaLlmrouterGeminiKeyCanary"
)

var (
	testConnection = sdkgo.ConnectionRef{Provider: "llm", Name: "llm-test"}
	testAPIKeys    = []string{openAITestKey, anthropicTestKey, geminiTestKey}
	// routedProviders lists the providers with their fake-provider dialects and keys.
	routedProviders = []routedProvider{
		{provider: llmrouter.ProviderOpenAI, connectorID: "openai", credentialField: "openai_api_key", apiKey: openAITestKey,
			dialect: providerdialecttest.NewOpenAIResponsesDialect(), sharedSuiteKeyPrefix: "", nonDefaultConnectionModel: "gpt-6-astra"},
		{provider: llmrouter.ProviderAnthropic, connectorID: "claude", credentialField: "anthropic_api_key", apiKey: anthropicTestKey,
			dialect: providerdialecttest.NewClaudeMessagesDialect(), sharedSuiteKeyPrefix: "sk-ant-", nonDefaultConnectionModel: "claude-opus-5-5"},
		{provider: llmrouter.ProviderGemini, connectorID: "gemini", credentialField: "gemini_api_key", apiKey: geminiTestKey,
			dialect: providerdialecttest.NewGeminiGenerateContentDialect(), sharedSuiteKeyPrefix: "AIza", nonDefaultConnectionModel: "gemini-3.5-pro"},
	}
)

type routedProvider struct {
	provider        llmrouter.Provider
	connectorID     string
	credentialField string
	apiKey          string
	dialect         llmtest.ProviderDialect
	// sharedSuiteKeyPrefix makes the llmtest suites' fixed sk- canary a key this provider's guard accepts.
	sharedSuiteKeyPrefix string
	// nonDefaultConnectionModel differs from the provider connector's default and the dialect's AlternateModel.
	nonDefaultConnectionModel string
}

// exchangeSuiteDialect keeps the provider-default connection model, because the Query New builds always has it.
func (routed routedProvider) exchangeSuiteDialect() llmtest.ProviderDialect {
	dialect := routed.dialect
	dialect.CredentialHeader.Prefix += routed.sharedSuiteKeyPrefix
	return dialect
}

// dexScenarioDialect uses a non-default connection model, so the scenarios prove the router sends it.
func (routed routedProvider) dexScenarioDialect() llmtest.ProviderDialect {
	dialect := routed.exchangeSuiteDialect()
	dialect.ConnectionModel = routed.nonDefaultConnectionModel
	return dialect
}

// withAPIKey returns credentials that add this provider, when absent, and hold apiKey in its field only.
func (routed routedProvider) withAPIKey(credentials llmrouter.Credentials, apiKey string) llmrouter.Credentials {
	if !credentials.HasAuthMethod(string(routed.provider)) {
		credentials.AuthMethodIDs = append(slices.Clone(credentials.AuthMethodIDs), string(routed.provider))
	}
	switch routed.provider {
	case llmrouter.ProviderOpenAI:
		credentials.OpenAIAPIKey = sdkgo.NewSecretString(apiKey)
	case llmrouter.ProviderAnthropic:
		credentials.AnthropicAPIKey = sdkgo.NewSecretString(apiKey)
	case llmrouter.ProviderGemini:
		credentials.GeminiAPIKey = sdkgo.NewSecretString(apiKey)
	}
	return credentials
}

func (routed routedProvider) generatedReply(text string) llmtest.FakeReply {
	return routed.dialect.GeneratedReply(llmtest.GeneratedReply{
		Text: text, ServedModel: routed.dialect.AlternateModel, ResponseID: "resp-" + routed.connectorID,
		Usage: llm.Usage{InputTokens: 9, OutputTokens: 4, TotalTokens: 13},
	})
}

// providerFakes holds one fake provider per route, each expecting only its own key.
type providerFakes struct {
	providers map[llmrouter.Provider]*llmtest.FakeProvider
}

func newProviderFakes(t *testing.T) *providerFakes {
	t.Helper()
	fakes := &providerFakes{providers: map[llmrouter.Provider]*llmtest.FakeProvider{}}
	for _, routed := range routedProviders {
		fakes.providers[routed.provider] = llmtest.NewFakeProvider(t, routed.dialect.CredentialHeader, routed.apiKey)
	}
	return fakes
}

func (fakes *providerFakes) options() []llmrouter.Option {
	options := make([]llmrouter.Option, 0, len(fakes.providers))
	for provider, fake := range fakes.providers {
		options = append(options, llmrouter.WithProviderBaseURLForTest(provider, fake.BaseURL()))
	}
	return options
}

func (fakes *providerFakes) requestCounts() map[llmrouter.Provider]int {
	counts := map[llmrouter.Provider]int{}
	for provider, fake := range fakes.providers {
		counts[provider] = len(fake.Requests())
	}
	return counts
}

// allTestAPIKeys adds all three providers, in the order openai, anthropic, gemini, each with its key.
func allTestAPIKeys() llmrouter.Credentials {
	return llmrouter.Credentials{
		AuthMethodIDs: []string{"openai", "anthropic", "gemini"},
		OpenAIAPIKey:  sdkgo.NewSecretString(openAITestKey), AnthropicAPIKey: sdkgo.NewSecretString(anthropicTestKey),
		GeminiAPIKey: sdkgo.NewSecretString(geminiTestKey),
	}
}

func staticCredentials(credentials llmrouter.Credentials) sdkgo.StaticCredentialProvider[llmrouter.Credentials] {
	return sdkgo.StaticCredentialProvider[llmrouter.Credentials]{testConnection: credentials}
}

func newRoutedClient(
	t *testing.T, fakes *providerFakes, config llmrouter.Config, credentials sdkgo.CredentialProvider[llmrouter.Credentials],
) *llmrouter.Client {
	t.Helper()
	client, err := llmrouter.New(config, credentials, fakes.options()...)
	require.NoError(t, err)
	return client
}

// runGenerateText runs one attempt and requires that no API key appears in any rendering of its outcome.
func runGenerateText(t *testing.T, client *llmrouter.Client, request llmrouter.GenerateTextRequest) (llmrouter.GenerateTextResult, error) {
	t.Helper()
	stepExecutionID := fmt.Sprintf("llm-step-%d", time.Now().UnixNano())
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("llm-flow", stepExecutionID), client.GenerateText(), testConnection, request)
	requireNoAPIKey(t, result, err)
	return result, err
}

func requireNoAPIKey(t *testing.T, result llmrouter.GenerateTextResult, err error) {
	t.Helper()
	encoded, marshalErr := json.Marshal(result)
	require.NoError(t, marshalErr)
	renderings := []string{string(encoded), fmt.Sprintf("%v", result), fmt.Sprintf("%+v", result), fmt.Sprintf("%#v", result)}
	if err != nil {
		renderings = append(renderings, err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err))
	}
	for _, rendering := range renderings {
		for _, apiKey := range testAPIKeys {
			// A boolean assertion keeps the key out of the failure message.
			require.False(t, strings.Contains(rendering, apiKey), "a Result, Failure, Receipt, or error contains an API key")
		}
	}
}

func userRequest(model string, text string) llmrouter.GenerateTextRequest {
	return llmrouter.GenerateTextRequest{Model: model, Messages: []llm.Message{{Role: llm.MessageRoleUser, Text: text}}}
}

func TestModelSelectionFollowsTheProviderModelGrammar(t *testing.T) {
	for _, testCase := range []struct {
		name, connectionModel, requestModel string
		provider                            llmrouter.Provider
		sentModel                           string
	}{
		{"connection model applies without a request model", "anthropic/claude-opus-5-5", "", llmrouter.ProviderAnthropic, "claude-opus-5-5"},
		{"a request model overrides the connection model", "anthropic/claude-opus-5-5", "openai/gpt-6-luna", llmrouter.ProviderOpenAI, "gpt-6-luna"},
		{"a blank request model inherits the connection model", "gemini/gemini-3.8-flash", " \t", llmrouter.ProviderGemini, "gemini-3.8-flash"},
		{"surrounding whitespace is trimmed", "openai", "  gemini/gemini-3.8-flash\t", llmrouter.ProviderGemini, "gemini-3.8-flash"},
		{"a provider alone uses the Claude connector's default", "openai", "anthropic", llmrouter.ProviderAnthropic, "claude-sonnet-5"},
		{"a provider alone uses the OpenAI connector's default", "openai", "", llmrouter.ProviderOpenAI, "gpt-6-sol"},
		{"a provider alone uses the Gemini connector's default", "gemini", "", llmrouter.ProviderGemini, "gemini-3.5-flash-lite"},
		{"Gemini accepts a models/ path segment", "openai", "gemini/models/gemini-3.8-flash", llmrouter.ProviderGemini, "gemini-3.8-flash"},
		{"OpenAI keeps a fine-tune ID with colons", "openai", "openai/ft:gpt-6-sol:acme::abc123", llmrouter.ProviderOpenAI, "ft:gpt-6-sol:acme::abc123"},
		{"only the first slash separates the provider", "openai", "anthropic/claude/with-slash", llmrouter.ProviderAnthropic, "claude/with-slash"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fakes := newProviderFakes(t)
			routed := routedProviderFor(t, testCase.provider)
			fakes.providers[testCase.provider].EnqueueReplies(routed.generatedReply("Routed."))
			client := newRoutedClient(t, fakes, llmrouter.Config{Model: testCase.connectionModel}, staticCredentials(allTestAPIKeys()))
			result, err := runGenerateText(t, client, userRequest(testCase.requestModel, "Hello"))
			require.NoError(t, err)
			require.Equal(t, llmrouter.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, testCase.sentModel, result.Value.RequestedModel)
			require.Equal(t, routed.connectorID, result.Receipt.Provider)
			require.Equal(t, string(testCase.provider)+"/"+testCase.sentModel, llmrouter.QualifiedModel(result))
			requests := fakes.providers[testCase.provider].Requests()
			require.Len(t, requests, 1)
			sentModel, err := routed.dialect.ReadRequestModel(requests[0])
			require.NoError(t, err)
			require.Equal(t, testCase.sentModel, sentModel)
			for _, other := range routedProvidersExcept(testCase.provider) {
				require.Empty(t, fakes.providers[other].Requests(), "no other provider receives a request")
			}
		})
	}
}

func TestInvalidModelSelectionsSelectDefectWithoutARequest(t *testing.T) {
	const grammarMessage = "the model must be provider/model, where provider is openai, anthropic, or gemini, such as anthropic/claude-sonnet-5"
	for _, testCase := range []struct {
		name, requestModel, message string
	}{
		{"an uppercase prefix", "Anthropic/claude-canary-model", grammarMessage},
		{"a bare model ID", "claude-canary-model", grammarMessage},
		{"a provider with an empty model", "anthropic/", grammarMessage},
		{"a provider with a blank model", "anthropic/   ", grammarMessage},
		{"an unrouted provider", "mistral/mistral-canary-model", grammarMessage},
		{"a slash without a provider", "/claude-canary-model", grammarMessage},
		{"a padded prefix", "anthropic /claude-canary-model", grammarMessage},
		{"a model that fails the body rule", "openai/gpt canary model",
			"the openai model ID must be 1 to 256 printable ASCII characters without spaces"},
		{"a model that fails the path-segment rule", "gemini/gemini:canary!",
			`the gemini model ID must start with a letter or digit and contain at most 128 letters, digits, ".", "_", or "-"`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fakes := newProviderFakes(t)
			client := newRoutedClient(t, fakes, llmrouter.Config{Model: "anthropic/claude-sonnet-5"}, staticCredentials(allTestAPIKeys()))
			result, err := runGenerateText(t, client, userRequest(testCase.requestModel, "Hello"))
			require.NoError(t, err, "a selection problem selects defect instead of Retry")
			require.Equal(t, llmrouter.GenerateTextBranchDefect, result.Branch)
			require.Equal(t, &sdkgo.Failure{
				Kind: sdkgo.FailureValidation, Provider: "llm", Operation: "generateText", Message: testCase.message,
			}, result.Failure)
			require.NotContains(t, result.Failure.Message, "canary", "the message never repeats the value")
			require.Equal(t, "llm", result.Receipt.Provider)
			require.NoError(t, result.Receipt.CallID.Validate())
			require.Empty(t, result.Value.RequestedModel)
			require.Empty(t, llmrouter.QualifiedModel(result), "no provider served the call")
			require.Equal(t, map[llmrouter.Provider]int{
				llmrouter.ProviderOpenAI: 0, llmrouter.ProviderAnthropic: 0, llmrouter.ProviderGemini: 0,
			}, fakes.requestCounts())
		})
	}
}

func TestNewValidatesTheConnection(t *testing.T) {
	credentials := staticCredentials(allTestAPIKeys())
	_, err := llmrouter.New(llmrouter.Config{Model: "openai"}, nil)
	require.ErrorContains(t, err, "credential provider is required")
	for _, model := range []string{"claude-canary-model", "Anthropic/claude-canary-model", "anthropic/", "mistral/canary-model", "gemini/canary model!"} {
		_, err := llmrouter.New(llmrouter.Config{Model: model}, credentials)
		require.ErrorContains(t, err, "configuration model", "%q", model)
		require.NotContains(t, err.Error(), "canary", "the error never repeats the value")
	}
	// A blank model defers to the first added provider at each call.
	for _, model := range []string{"", " \t", "openai", "anthropic", "gemini", "anthropic/claude-opus-5-5", " gemini/models/gemini-3.8-flash "} {
		_, err := llmrouter.New(llmrouter.Config{Model: model}, credentials)
		require.NoError(t, err, "%q", model)
	}
	for _, workspaceID := range []string{"workspace-canary", "wrkspc_bad!", "wrkspc_01\r\nX-Injected: 1"} {
		_, err := llmrouter.New(llmrouter.Config{Model: "openai", AnthropicWorkspaceID: workspaceID}, credentials)
		require.ErrorContains(t, err, "anthropic route")
		require.ErrorContains(t, err, "workspaceId")
		require.NotContains(t, err.Error(), workspaceID, "the error never repeats the value")
	}
	_, err = llmrouter.New(llmrouter.Config{Model: "openai", AnthropicWorkspaceID: "wrkspc_01JwQvzr7rXLA5AGx3HKfFUJ"}, credentials)
	require.NoError(t, err)
	_, err = llmrouter.New(llmrouter.Config{Model: "openai", MaxResponseBytes: -1}, credentials)
	require.ErrorContains(t, err, "maxResponseBytes")
	_, err = llmrouter.New(llmrouter.Config{Model: "openai"}, credentials, nil)
	require.ErrorContains(t, err, "option is nil")
}

func TestNewRejectsAnHTTPClientThatOutlastsTheExecuteTimeout(t *testing.T) {
	credentials := staticCredentials(allTestAPIKeys())
	require.Equal(t, 900*time.Second, llmrouter.GenerateTextDefinition.StepDefaults.ExecuteMethodTimeout)
	for _, timeout := range []time.Duration{900 * time.Second, time.Hour} {
		_, err := llmrouter.New(llmrouter.Config{Model: "openai"}, credentials, llmrouter.WithHTTPClient(&http.Client{Timeout: timeout}))
		require.ErrorContains(t, err, "900-second generateText Execute timeout", "%s", timeout)
	}
	for _, timeout := range []time.Duration{0, 600 * time.Second, 899 * time.Second} {
		_, err := llmrouter.New(llmrouter.Config{Model: "openai"}, credentials, llmrouter.WithHTTPClient(&http.Client{Timeout: timeout}))
		require.NoError(t, err, "%s", timeout)
	}
}

func TestProviderBaseURLForTestAcceptsOnlyLoopbackHosts(t *testing.T) {
	credentials := staticCredentials(allTestAPIKeys())
	for _, baseURL := range []string{
		"https://api.openai.com.example.com", "http://192.0.2.10:8080", "https://attacker.example", "http://[::1]:7000",
		"http://127.0.0.2:8080", "http://user:secret@127.0.0.1:8080", "http://127.0.0.1:8080/?next=1", "http://127.0.0.1:8080/#top",
		"ftp://127.0.0.1:21", "127.0.0.1:8080", "http://LOCALHOST:8080",
	} {
		for _, routed := range routedProviders {
			_, err := llmrouter.New(llmrouter.Config{Model: "openai"}, credentials, llmrouter.WithProviderBaseURLForTest(routed.provider, baseURL))
			require.ErrorContains(t, err, "127.0.0.1 or localhost", "%s %s", routed.provider, baseURL)
			require.NotContains(t, err.Error(), "secret")
		}
	}
	for _, baseURL := range []string{"http://127.0.0.1:8080", "https://localhost:9000", "http://localhost:9000/v1"} {
		for _, routed := range routedProviders {
			_, err := llmrouter.New(llmrouter.Config{Model: "openai"}, credentials, llmrouter.WithProviderBaseURLForTest(routed.provider, baseURL))
			require.NoError(t, err, "%s %s", routed.provider, baseURL)
		}
	}
	for _, provider := range []llmrouter.Provider{"claude", "OpenAI", ""} {
		_, err := llmrouter.New(llmrouter.Config{Model: "openai"}, credentials, llmrouter.WithProviderBaseURLForTest(provider, "http://127.0.0.1:8080"))
		require.ErrorContains(t, err, "unknown provider", "%q", provider)
	}
}

func TestNewChecksTheRoutingDefinition(t *testing.T) {
	require.NoError(t, llmrouter.ValidateRouterDefinitionForTest(llmrouter.GenerateTextDefinition))
	for _, testCase := range []struct {
		name    string
		mutate  func(*sdkgo.QueryDefinition)
		message string
	}{
		{"async durability repeats the provider call", func(definition *sdkgo.QueryDefinition) {
			definition.StepDefaults.ExecuteDurability = dex.StepDurabilityAsync
		}, "sync Execute durability"},
		{"a heartbeat below Dex's minimum", func(definition *sdkgo.QueryDefinition) {
			definition.StepDefaults.HeartbeatTimeout = 5 * time.Second
		}, "at least 10 seconds"},
		{"an Execute timeout inside the provider request timeout", func(definition *sdkgo.QueryDefinition) {
			definition.StepDefaults.ExecuteMethodTimeout = 870 * time.Second
		}, "870-second request timeout"},
		{"a missing branch", func(definition *sdkgo.QueryDefinition) {
			definition.Branches = slices.Delete(definition.Branches, 2, 3)
		}, "llm.TextGenerationBranchDefinitions"},
		{"a required optional branch", func(definition *sdkgo.QueryDefinition) {
			definition.Branches[1].Optional = false
		}, "llm.TextGenerationBranchDefinitions"},
		{"another operation", func(definition *sdkgo.QueryDefinition) {
			definition.Operation.OperationID = "createResponse"
		}, "generateText"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			definition := llmrouter.GenerateTextDefinition
			definition.Branches = slices.Clone(definition.Branches)
			testCase.mutate(&definition)
			require.ErrorContains(t, llmrouter.ValidateRouterDefinitionForTest(definition), testCase.message)
		})
	}
}

// TestNewRejectsAProviderConnectorThatOutgrowsTheBudget covers an application that links a newer provider connector under MVS.
func TestNewRejectsAProviderConnectorThatOutgrowsTheBudget(t *testing.T) {
	longer := llmrouter.GenerateTextDefinition
	longer.StepDefaults.ExecuteMethodTimeout = 1200 * time.Second
	err := llmrouter.ValidateProviderRouteDefinitionForTest("claude", longer, llmrouter.GenerateTextDefinition)
	require.EqualError(t, err, "the llm connector cannot route to the claude connector linked into this application: "+
		"its generateText needs a longer Execute timeout than llm's 900 seconds. "+
		"Upgrade the llm connector, or require the claude connector version that the llm connector's go.mod lists.")

	otherBranches := llmrouter.GenerateTextDefinition
	otherBranches.Branches = append(slices.Clone(otherBranches.Branches), sdkgo.BranchDefinition{ID: "refused", Optional: true})
	err = llmrouter.ValidateProviderRouteDefinitionForTest("openai", otherBranches, llmrouter.GenerateTextDefinition)
	require.ErrorContains(t, err, "the llm connector cannot route to the openai connector linked into this application: its generateText declares other branches")

	require.NoError(t, llmrouter.ValidateProviderRouteDefinitionForTest("gemini", llmrouter.GenerateTextDefinition, llmrouter.GenerateTextDefinition))
}

func TestQualifiedModelRestoresTheSelectionPrefix(t *testing.T) {
	for connectorID, expected := range map[string]string{
		"openai": "openai/model-1", "claude": "anthropic/model-1", "gemini": "gemini/model-1", "llm": "", "mistral": "", "": "",
	} {
		result := llm.TextGenerationResult{Receipt: sdkgo.Receipt{Provider: connectorID}, Value: llm.TextGenerationResponse{RequestedModel: "model-1"}}
		require.Equal(t, expected, llmrouter.QualifiedModel(result), connectorID)
	}
	require.Empty(t, llmrouter.QualifiedModel(llm.TextGenerationResult{Receipt: sdkgo.Receipt{Provider: "claude"}}),
		"a result without a requested model has nothing to qualify")
}

// TestLocalConnectionDecodesOneToThreeProviders builds the connection from the files Dex Web Connections writes.
func TestLocalConnectionDecodesOneToThreeProviders(t *testing.T) {
	for _, credentials := range []map[string]any{
		{"auth_methods": []string{"anthropic"}, "anthropic_api_key": anthropicTestKey},
		{"auth_methods": []string{"gemini", "openai"}, "gemini_api_key": geminiTestKey, "openai_api_key": openAITestKey},
		{
			"auth_methods":   []string{"openai", "anthropic", "gemini"},
			"openai_api_key": openAITestKey, "anthropic_api_key": anthropicTestKey, "gemini_api_key": geminiTestKey,
		},
	} {
		for _, configuration := range []map[string]any{
			{}, {"model": "anthropic/claude-sonnet-5"},
			{"model": "anthropic", "anthropicWorkspaceId": "wrkspc_01JwQvzr7rXLA5AGx3HKfFUJ", "maxResponseBytes": 1 << 20},
		} {
			connection, err := llmrouter.NewLocalConnection(writeLocalConnection(t, configuration, credentials), testConnection.Name)
			require.NoError(t, err, "configuration %v", configuration)
			require.Equal(t, "llmrouter.Connection{[REDACTED]}", fmt.Sprintf("%+v", connection))
		}
	}
	_, err := llmrouter.NewLocalConnection(writeLocalConnection(t, map[string]any{"model": "claude-sonnet-5"}, map[string]any{}), testConnection.Name)
	require.ErrorContains(t, err, "provider/model", "a bare model fails at startup")
	_, err = llmrouter.NewLocalConnection(
		writeLocalConnection(t, map[string]any{"anthropicWorkspaceId": "workspace-canary"}, map[string]any{}), testConnection.Name)
	require.ErrorContains(t, err, "workspaceId", "a bad workspace ID fails at startup even without a model")
}

func writeLocalConnection(t *testing.T, configuration map[string]any, credentials map[string]any) *localconfig.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "connections.json")
	writeLocalConnectionFile(t, path, configuration, credentials)
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	return store
}

// writeLocalConnectionFile writes one llm connection record to path, as Dex Web Connections saves it.
func writeLocalConnectionFile(t *testing.T, path string, configuration map[string]any, credentials map[string]any) {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": llmrouter.ConnectorID, "connectionName": testConnection.Name, "provider": "llm",
			"modulePath": "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm", "moduleVersion": "v0.2.0",
			"configuration": configuration, "credentials": credentials,
		}},
	})
	require.NoError(t, err)
	temporary := path + ".tmp"
	require.NoError(t, os.WriteFile(temporary, encoded, 0o600))
	// Renaming replaces the file atomically, as Dex Web does, so a concurrent read never sees half of it.
	require.NoError(t, os.Rename(temporary, path))
}

func routedProviderFor(t *testing.T, provider llmrouter.Provider) routedProvider {
	t.Helper()
	for _, routed := range routedProviders {
		if routed.provider == provider {
			return routed
		}
	}
	t.Fatalf("no routed provider %q", provider)
	return routedProvider{}
}

func routedProvidersExcept(provider llmrouter.Provider) []llmrouter.Provider {
	var others []llmrouter.Provider
	for _, routed := range routedProviders {
		if routed.provider != provider {
			others = append(others, routed.provider)
		}
	}
	return others
}
