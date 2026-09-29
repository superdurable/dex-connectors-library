// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmrouter_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// providerDisplayNames are the manifest displayNames of the auth methods that add each provider.
var providerDisplayNames = map[llmrouter.Provider]string{
	llmrouter.ProviderOpenAI: "OpenAI", llmrouter.ProviderAnthropic: "Claude", llmrouter.ProviderGemini: "Gemini",
}

var noProviderRequests = map[llmrouter.Provider]int{llmrouter.ProviderOpenAI: 0, llmrouter.ProviderAnthropic: 0, llmrouter.ProviderGemini: 0}

func TestBlankModelUsesTheFirstAddedProvidersDefaultModel(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		authMethodIDs []string
		requestModel  string
		provider      llmrouter.Provider
		sentModel     string
	}{
		{"Claude added first", []string{"anthropic", "openai", "gemini"}, "", llmrouter.ProviderAnthropic, "claude-sonnet-5"},
		{"Gemini added first", []string{"gemini", "openai"}, " \t", llmrouter.ProviderGemini, "gemini-3.5-flash-lite"},
		{"OpenAI alone", []string{"openai"}, "", llmrouter.ProviderOpenAI, "gpt-6-sol"},
		{"a request model overrides the first added provider", []string{"anthropic", "gemini"}, "gemini/gemini-3.8-flash",
			llmrouter.ProviderGemini, "gemini-3.8-flash"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fakes := newProviderFakes(t)
			routed := routedProviderFor(t, testCase.provider)
			fakes.providers[testCase.provider].EnqueueReplies(routed.generatedReply("First added."))
			credentials := allTestAPIKeys()
			credentials.AuthMethodIDs = testCase.authMethodIDs
			client := newRoutedClient(t, fakes, llmrouter.Config{}, staticCredentials(credentials))
			result, err := runGenerateText(t, client, userRequest(testCase.requestModel, "Hello"))
			require.NoError(t, err)
			require.Equal(t, llmrouter.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
			require.Equal(t, string(testCase.provider)+"/"+testCase.sentModel, llmrouter.QualifiedModel(result))
			requests := fakes.providers[testCase.provider].Requests()
			require.Len(t, requests, 1)
			sentModel, err := routed.dialect.ReadRequestModel(requests[0])
			require.NoError(t, err)
			require.Equal(t, testCase.sentModel, sentModel)
			for _, other := range routedProvidersExcept(testCase.provider) {
				require.Empty(t, fakes.providers[other].Requests())
			}
		})
	}
}

func TestModelOfAProviderNotAddedSelectsDefectWithoutARequest(t *testing.T) {
	for _, routed := range routedProviders {
		// Every key stays in the credentials, so only the provider list keeps this provider's key from being sent.
		credentials := allTestAPIKeys()
		credentials.AuthMethodIDs = nil
		for _, other := range routedProvidersExcept(routed.provider) {
			credentials.AuthMethodIDs = append(credentials.AuthMethodIDs, string(other))
		}
		expectedMessage := fmt.Sprintf("the model selects %s, but the connection has not added the %s provider; add %s to the connection",
			routed.provider, providerDisplayNames[routed.provider], providerDisplayNames[routed.provider])
		for name, models := range map[string]struct{ connectionModel, requestModel, requestedModel string }{
			"a request model":    {"", string(routed.provider) + "/" + routed.dialect.AlternateModel, routed.dialect.AlternateModel},
			"a connection model": {string(routed.provider), "", routed.dialect.ConnectionModel},
		} {
			t.Run(string(routed.provider)+" "+name, func(t *testing.T) {
				fakes := newProviderFakes(t)
				client := newRoutedClient(t, fakes, llmrouter.Config{Model: models.connectionModel}, staticCredentials(credentials))
				result, err := runGenerateText(t, client, userRequest(models.requestModel, "Hello"))
				require.NoError(t, err, "a provider that is not added selects defect instead of Retry")
				require.Equal(t, llmrouter.GenerateTextBranchDefect, result.Branch)
				require.Equal(t, &sdkgo.Failure{
					Kind: sdkgo.FailureAuthentication, Provider: "llm", Operation: "generateText", Message: expectedMessage,
				}, result.Failure)
				require.Equal(t, models.requestedModel, result.Value.RequestedModel, "the model was valid, so it is reported")
				require.Equal(t, "llm", result.Receipt.Provider)
				require.Equal(t, noProviderRequests, fakes.requestCounts(), "the first added provider is never a fallback")
			})
		}
	}
}

func TestConnectionWithoutAUsableFirstProviderSelectsDefect(t *testing.T) {
	for _, testCase := range []struct {
		name, connectionModel string
		authMethodIDs         []string
		message               string
	}{
		{"no provider and no model", "", nil, "the connection has added no provider; add OpenAI, Claude, or Gemini to the connection"},
		{"no provider and a model", "openai", nil,
			"the model selects openai, but the connection has not added the OpenAI provider; add OpenAI to the connection"},
		{"an unknown first provider", "", []string{"mistral", "openai"}, "the connection's first provider is not openai, anthropic, or gemini"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fakes := newProviderFakes(t)
			credentials := allTestAPIKeys()
			credentials.AuthMethodIDs = testCase.authMethodIDs
			client := newRoutedClient(t, fakes, llmrouter.Config{Model: testCase.connectionModel}, staticCredentials(credentials))
			result, err := runGenerateText(t, client, userRequest("", "Hello"))
			require.NoError(t, err)
			require.Equal(t, llmrouter.GenerateTextBranchDefect, result.Branch)
			require.Equal(t, &sdkgo.Failure{
				Kind: sdkgo.FailureAuthentication, Provider: "llm", Operation: "generateText", Message: testCase.message,
			}, result.Failure)
			require.Equal(t, noProviderRequests, fakes.requestCounts())
		})
	}
}

// TestProviderListChangesTakeEffectWithoutARestart rewrites the connection file as Dex Web does when providers are removed or reordered.
func TestProviderListChangesTakeEffectWithoutARestart(t *testing.T) {
	fakes := newProviderFakes(t)
	openAIRoute, claudeRoute := routedProviderFor(t, llmrouter.ProviderOpenAI), routedProviderFor(t, llmrouter.ProviderAnthropic)
	connectionsPath := filepath.Join(t.TempDir(), "connections.json")
	writeLocalConnectionFile(t, connectionsPath, map[string]any{}, map[string]any{
		"auth_methods": []string{"openai", "anthropic"}, "openai_api_key": openAITestKey, "anthropic_api_key": anthropicTestKey,
	})
	store, err := localconfig.LoadFile(connectionsPath)
	require.NoError(t, err)
	connection, err := llmrouter.NewLocalConnection(store, testConnection.Name, fakes.options()...)
	require.NoError(t, err)

	fakes.providers[llmrouter.ProviderOpenAI].EnqueueReplies(openAIRoute.generatedReply("OpenAI first."))
	result := runConnectionGenerateText(t, connection, userRequest("", "Hello"))
	require.Equal(t, llmrouter.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "openai/gpt-6-sol", llmrouter.QualifiedModel(result))

	// Removing OpenAI drops its key, and Claude becomes the first added provider.
	writeLocalConnectionFile(t, connectionsPath, map[string]any{}, map[string]any{
		"auth_methods": []string{"anthropic"}, "anthropic_api_key": anthropicTestKey,
	})
	fakes.providers[llmrouter.ProviderAnthropic].EnqueueReplies(claudeRoute.generatedReply("Claude first."))
	result = runConnectionGenerateText(t, connection, userRequest("", "Hello"))
	require.Equal(t, llmrouter.GenerateTextBranchGenerated, result.Branch, "failure: %+v", result.Failure)
	require.Equal(t, "anthropic/claude-sonnet-5", llmrouter.QualifiedModel(result))

	result = runConnectionGenerateText(t, connection, userRequest("openai/gpt-6-luna", "Hello"))
	require.Equal(t, llmrouter.GenerateTextBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, "the model selects openai, but the connection has not added the OpenAI provider; add OpenAI to the connection",
		result.Failure.Message)
	require.Len(t, fakes.providers[llmrouter.ProviderOpenAI].Requests(), 1, "the removed provider receives no further request")
	require.Len(t, fakes.providers[llmrouter.ProviderAnthropic].Requests(), 1)
}

// TestGeneratedCredentialsValidateTheAddedProviders checks the generated rules that every local credential read applies.
func TestGeneratedCredentialsValidateTheAddedProviders(t *testing.T) {
	openAIOnly := llmrouter.Credentials{AuthMethodIDs: []string{"openai"}, OpenAIAPIKey: sdkgo.NewSecretString(openAITestKey)}
	require.NoError(t, openAIOnly.Validate())
	require.NoError(t, allTestAPIKeys().Validate())
	require.True(t, openAIOnly.HasAuthMethod("openai"))
	require.False(t, openAIOnly.HasAuthMethod("anthropic"))
	for _, testCase := range []struct {
		name        string
		credentials llmrouter.Credentials
		message     string
	}{
		{"no auth methods, as a v0.1.0 record has", llmrouter.Credentials{OpenAIAPIKey: sdkgo.NewSecretString(openAITestKey)},
			"credential auth_methods is required"},
		{"an added provider without its key", llmrouter.Credentials{
			AuthMethodIDs: []string{"openai", "anthropic"}, OpenAIAPIKey: sdkgo.NewSecretString(openAITestKey),
		}, "credential anthropic_api_key is required"},
		{"a provider added twice", llmrouter.Credentials{
			AuthMethodIDs: []string{"openai", "openai"}, OpenAIAPIKey: sdkgo.NewSecretString(openAITestKey),
		}, "credential auth_methods must be unique"},
		{"an undeclared auth method", llmrouter.Credentials{AuthMethodIDs: []string{"mistral"}},
			"credential auth_methods contains an undeclared auth method"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.EqualError(t, testCase.credentials.Validate(), testCase.message)
		})
	}
}

// TestLocalCredentialsWithoutAValidProviderListSelectDefect covers records that v0.1.0 or a single-selection host saved.
func TestLocalCredentialsWithoutAValidProviderListSelectDefect(t *testing.T) {
	for name, credentials := range map[string]map[string]any{
		"a v0.1.0 record without auth_methods": {"openai_api_key": openAITestKey, "anthropic_api_key": anthropicTestKey},
		"a single-selection auth_method":       {"auth_method": "openai", "openai_api_key": openAITestKey},
		"auth_methods that is not a list":      {"auth_methods": "openai", "openai_api_key": openAITestKey},
		"an added provider without its key":    {"auth_methods": []string{"openai", "anthropic"}, "openai_api_key": openAITestKey},
	} {
		t.Run(name, func(t *testing.T) {
			fakes := newProviderFakes(t)
			store := writeLocalConnection(t, map[string]any{"model": "openai"}, credentials)
			connection, err := llmrouter.NewLocalConnection(store, testConnection.Name, fakes.options()...)
			require.NoError(t, err, "credentials are read per call, so the Worker still starts")
			result := runConnectionGenerateText(t, connection, userRequest("", "Hello"))
			require.Equal(t, llmrouter.GenerateTextBranchDefect, result.Branch)
			require.Equal(t, &sdkgo.Failure{
				Kind: sdkgo.FailureAuthentication, Provider: "llm", Operation: "generateText", Message: "connection credentials are unavailable",
			}, result.Failure)
			require.Equal(t, noProviderRequests, fakes.requestCounts())
		})
	}
}

// runConnectionGenerateText runs one attempt of connection's routing Query and requires that no API key appears in its outcome.
func runConnectionGenerateText(t *testing.T, connection llmrouter.Connection, request llmrouter.GenerateTextRequest) llmrouter.GenerateTextResult {
	t.Helper()
	ctx := testsupport.NewDexContext("llm-connection-flow", fmt.Sprintf("llm-connection-step-%d", time.Now().UnixNano()))
	result, err := sdkgo.RunQuery(ctx, llmrouter.ConnectionGenerateTextQueryForTest(connection), testConnection, request)
	requireNoAPIKey(t, result, err)
	require.NoError(t, err)
	return result
}
