// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// manifestDocument is the part of connector.yaml these tests read.
type manifestDocument struct {
	Spec struct {
		Configuration struct {
			Fields []manifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Type           string          `yaml:"type"`
			ConnectionKind string          `yaml:"connectionKind"`
			Selection      string          `yaml:"selection"`
			Methods        []any           `yaml:"methods"`
			Fields         []manifestField `yaml:"fields"`
			Guide          struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
		} `yaml:"auth"`
		Studio struct {
			Setup struct {
				BackendCapabilities []string `yaml:"backendCapabilities"`
			} `yaml:"setup"`
			Commands []manifestStudioCommand `yaml:"commands"`
			Units    []struct {
				ID                  string   `yaml:"id"`
				Description         string   `yaml:"description"`
				BackendCapabilities []string `yaml:"backendCapabilities"`
			} `yaml:"units"`
		} `yaml:"studio"`
	} `yaml:"spec"`
}

type manifestField struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Description string   `yaml:"description"`
	Required    bool     `yaml:"required"`
	Enum        []string `yaml:"enum"`
	Default     any      `yaml:"default"`
	StudioUnit  *struct {
		Unit string `yaml:"unit"`
		Port string `yaml:"port"`
	} `yaml:"studioUnit"`
}

type manifestStudioCommand struct {
	ID         string `yaml:"id"`
	Capability string `yaml:"capability"`
	Request    struct {
		Method     string `yaml:"method"`
		URL        string `yaml:"url"`
		Credential struct {
			Field  string `yaml:"field"`
			Scheme string `yaml:"scheme"`
			Header string `yaml:"header"`
		} `yaml:"credential"`
	} `yaml:"request"`
}

// providerOrder is the provider enum in manifest order.
var providerOrder = []Provider{
	ProviderOpenai, ProviderAnthropic, ProviderGemini, ProviderQwen, ProviderDeepseek, ProviderMeta, ProviderMistral, ProviderKimi, ProviderXai,
}

// listCommandProviders maps every Studio list command to the provider whose key it sends.
var listCommandProviders = map[string]Provider{
	"listOpenAIModels": ProviderOpenai, "listAnthropicModels": ProviderAnthropic,
	"listGeminiModels": ProviderGemini, "listGeminiOpenAICompatibleModels": ProviderGemini,
	"listQwenModels": ProviderQwen, "listQwenReasoningModels": ProviderQwen,
	"listQwenModelsHongKong": ProviderQwen, "listQwenReasoningModelsHongKong": ProviderQwen,
	"listDeepSeekModels": ProviderDeepseek, "listMetaModels": ProviderMeta, "listMistralModels": ProviderMistral,
	"listKimiModels": ProviderKimi, "listKimiModelsChina": ProviderKimi,
	"listXAIModels": ProviderXai, "listXAIModelsUS": ProviderXai,
}

// TestManifestEnumsEqualTheServedProvidersAndRegions keeps connector.yaml and providerAPIs one list.
func TestManifestEnumsEqualTheServedProvidersAndRegions(t *testing.T) {
	fields := readManifestFields(t)
	var providers []Provider
	for _, value := range fields["provider"].Enum {
		providers = append(providers, Provider(value))
	}
	require.Equal(t, providerOrder, providers)
	require.Len(t, providerAPIs, len(providerOrder))
	servedRegions := map[Region]bool{}
	for _, provider := range providerOrder {
		api, isServed := providerAPIs[provider]
		require.True(t, isServed, provider)
		require.Contains(t, api.regionBaseURLs, RegionGlobal, "%s serves the default region", provider)
		require.NotEmpty(t, api.defaultModel, provider)
		require.Less(t, api.requestTimeout, GenerateTextDefinition.StepDefaults.ExecuteMethodTimeout, provider)
		for region, baseURL := range api.regionBaseURLs {
			servedRegions[region] = true
			parsed, err := url.Parse(baseURL)
			require.NoError(t, err)
			require.Equal(t, "https", parsed.Scheme, "%s %s", provider, region)
		}
		_, err := api.newWireFormat(&Config{Provider: provider})
		require.NoError(t, err, provider)
	}
	var regions []Region
	for _, value := range fields["region"].Enum {
		regions = append(regions, Region(value))
		require.True(t, servedRegions[Region(value)], "some provider serves region %s", value)
	}
	require.Len(t, servedRegions, len(regions), "the region enum lists every served region")
}

// TestConnectionFieldsGuideTheUser enumerates every field the Connections form shows.
func TestConnectionFieldsGuideTheUser(t *testing.T) {
	manifest := readManifest(t)
	var names []string
	for _, field := range manifest.Spec.Configuration.Fields {
		names = append(names, field.Name)
		require.NotEmpty(t, field.Description, field.Name)
		require.NotEqual(t, "url", field.Type, "provider hosts are fixed, so no URL field is shown")
	}
	require.Equal(t, []string{"provider", "model", "region", "anthropicWorkspaceId", "maxResponseBytes"}, names)
	fields := readManifestFields(t)

	provider := fields["provider"]
	require.Equal(t, "enum", provider.Type)
	require.True(t, provider.Required, "a connection names exactly one provider")
	require.Nil(t, provider.Default, "no provider is a safe default for a key")
	for _, value := range provider.Enum {
		require.Contains(t, provider.Description, value+" (", "the provider description names %s", value)
	}

	model := fields["model"]
	require.False(t, model.Required)
	require.Nil(t, model.Default, "the default model depends on the provider")
	require.NotNil(t, model.StudioUnit, "the connection form renders the model picker for the default model")
	require.Equal(t, UIUnitModelPicker, model.StudioUnit.Unit)
	require.Equal(t, UIModelPickerPortModel, model.StudioUnit.Port)
	require.Contains(t, model.Description, "never prefixed with the provider")
	for _, provider := range providerOrder {
		require.Contains(t, model.Description, fmt.Sprintf("%s for %s", providerAPIs[provider].defaultModel, provider))
	}

	region := fields["region"]
	require.Equal(t, "global", region.Default)
	require.False(t, region.Required)
	for _, provider := range providerOrder {
		for servedRegion := range providerAPIs[provider].regionBaseURLs {
			if servedRegion != RegionGlobal {
				require.Contains(t, region.Description, string(servedRegion), "the region description names %s %s", provider, servedRegion)
				require.Contains(t, region.Description, string(provider))
			}
		}
	}

	workspace := fields["anthropicWorkspaceId"]
	require.False(t, workspace.Required, "a key scoped to one workspace needs no workspace ID")
	require.Contains(t, workspace.Description, "Settings > Workspaces")
	require.Contains(t, workspace.Description, "leave it blank")
	require.Contains(t, workspace.Description, "Only provider anthropic")

	maxResponseBytes := fields["maxResponseBytes"]
	require.Nil(t, maxResponseBytes.Default, "the response limit depends on the provider")
	require.Equal(t, int64(64<<20), providerAPIs[ProviderDeepseek].defaultMaxResponseBytes)
	require.Contains(t, maxResponseBytes.Description, "64 MiB for deepseek")
	for _, provider := range providerOrder {
		if provider != ProviderDeepseek {
			require.Equal(t, int64(8<<20), providerAPIs[provider].defaultMaxResponseBytes, provider)
		}
	}
	require.Contains(t, maxResponseBytes.Description, "8 MiB for every other provider")
}

// TestAuthorizationHoldsOneAPIKeyWithAGuideForEveryProvider replaces the former per-provider auth methods.
func TestAuthorizationHoldsOneAPIKeyWithAGuideForEveryProvider(t *testing.T) {
	auth := readManifest(t).Spec.Auth
	require.Equal(t, "apiKey", auth.Type)
	require.Equal(t, "llm-api-key", auth.ConnectionKind)
	require.Empty(t, auth.Selection, "one connection holds one key")
	require.Empty(t, auth.Methods, "the connection names its provider in configuration, not in auth methods")
	require.Len(t, auth.Fields, 1)
	key := auth.Fields[0]
	require.Equal(t, "api_key", key.Name)
	require.Equal(t, "secretString", key.Type)
	require.True(t, key.Required)
	require.Contains(t, key.Description, "sent only to the chosen provider's API host")
	require.True(t, strings.HasPrefix(auth.Guide.StartURL, "https://"))
	steps := strings.Join(auth.Guide.Steps, " ")
	for _, keyPage := range []string{
		"https://platform.openai.com/api-keys", "https://platform.claude.com/settings/keys", "https://aistudio.google.com/api-keys",
		"https://bailian.console.aliyun.com/", "https://platform.deepseek.com/api_keys", "https://dev.meta.ai/",
		"https://console.mistral.ai/api-keys", "https://platform.kimi.ai/", "https://console.x.ai/",
	} {
		require.Contains(t, steps, keyPage, "the guide links every provider's key page")
	}
	require.Contains(t, steps, "api_key")
	require.Contains(t, steps, "anthropicWorkspaceId")
	require.Contains(t, steps, "revoke")
}

// TestEachListCommandSendsTheKeyOnlyToItsProvidersAPIHost binds every command to a host the provider also generates on.
func TestEachListCommandSendsTheKeyOnlyToItsProvidersAPIHost(t *testing.T) {
	studio := readManifest(t).Spec.Studio
	require.Equal(t, []string{"use.configuration.write", "llm.models-list", "connection.write"}, studio.Setup.BackendCapabilities)
	require.Len(t, studio.Units, 1)
	require.Equal(t, UIUnitModelPicker, studio.Units[0].ID)
	require.Equal(t, []string{"llm.models-list"}, studio.Units[0].BackendCapabilities)
	require.Contains(t, studio.Units[0].Description, "blank")
	commandIDs := make([]string, 0, len(studio.Commands))
	for _, command := range studio.Commands {
		commandIDs = append(commandIDs, command.ID)
		provider, isListCommand := listCommandProviders[command.ID]
		require.True(t, isListCommand, "every Studio command is one provider's model list: %s", command.ID)
		require.Equal(t, "llm.models-list", command.Capability)
		require.Equal(t, "GET", command.Request.Method)
		require.Equal(t, "api_key", command.Request.Credential.Field)
		listURL, err := url.Parse(command.Request.URL)
		require.NoError(t, err)
		require.Equal(t, "https", listURL.Scheme)
		var providerHosts []string
		for _, baseURL := range providerAPIs[provider].regionBaseURLs {
			parsed, err := url.Parse(baseURL)
			require.NoError(t, err)
			providerHosts = append(providerHosts, parsed.Host)
		}
		require.Contains(t, providerHosts, listURL.Host, "%s sends the key to a %s API host", command.ID, provider)
		if command.ID == "listGeminiModels" {
			require.Equal(t, "header", command.Request.Credential.Scheme)
			require.Equal(t, "x-goog-api-key", command.Request.Credential.Header)
		} else {
			require.Equal(t, "bearer", command.Request.Credential.Scheme, command.ID)
		}
	}
	expectedIDs := make([]string, 0, len(listCommandProviders))
	for commandID := range listCommandProviders {
		expectedIDs = append(expectedIDs, commandID)
	}
	slices.Sort(expectedIDs)
	slices.Sort(commandIDs)
	require.Equal(t, expectedIDs, commandIDs)
}

func readManifest(t *testing.T) manifestDocument {
	t.Helper()
	contents, err := os.ReadFile("connector.yaml")
	require.NoError(t, err)
	var manifest manifestDocument
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

func readManifestFields(t *testing.T) map[string]manifestField {
	t.Helper()
	fields := map[string]manifestField{}
	for _, field := range readManifest(t).Spec.Configuration.Fields {
		fields[field.Name] = field
	}
	return fields
}
