// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llmrouter_test

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	claude "github.com/superdurable/dex-connectors-library/connectors/anthropic"
	"github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	"github.com/superdurable/dex-connectors-library/connectors/openai"
	llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	"gopkg.in/yaml.v3"
)

// studioManifest is the part of a connector manifest these tests read.
type studioManifest struct {
	Spec struct {
		Configuration struct {
			Fields []manifestField `yaml:"fields"`
		} `yaml:"configuration"`
		Auth struct {
			Fields []manifestField `yaml:"fields"`
			Guide  struct {
				StartURL string   `yaml:"startURL"`
				Steps    []string `yaml:"steps"`
			} `yaml:"guide"`
		} `yaml:"auth"`
		Studio struct {
			Setup struct {
				BackendCapabilities []string `yaml:"backendCapabilities"`
			} `yaml:"setup"`
			Commands []studioCommand `yaml:"commands"`
			Units    []struct {
				ID                  string   `yaml:"id"`
				Description         string   `yaml:"description"`
				BackendCapabilities []string `yaml:"backendCapabilities"`
			} `yaml:"units"`
		} `yaml:"studio"`
	} `yaml:"spec"`
}

type manifestField struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Description string `yaml:"description"`
}

type studioCommand struct {
	ID         string         `yaml:"id"`
	Capability string         `yaml:"capability"`
	Request    map[string]any `yaml:"request"`
}

// providerListCommands maps each llm list command to the pinned provider connector command it copies.
var providerListCommands = []struct {
	commandID, providerModule, providerCommandID, host, credentialField string
}{
	{"listOpenAIModels", "github.com/superdurable/dex-connectors-library/connectors/openai", "listModels", "api.openai.com", "openai_api_key"},
	{"listAnthropicModels", "github.com/superdurable/dex-connectors-library/connectors/anthropic", "listModels", "api.anthropic.com", "anthropic_api_key"},
	{"listGeminiModels", "github.com/superdurable/dex-connectors-library/connectors/google/gemini", "listModels",
		"generativelanguage.googleapis.com", "gemini_api_key"},
	{"listGeminiOpenAICompatibleModels", "github.com/superdurable/dex-connectors-library/connectors/google/gemini",
		"listOpenAICompatibleModels", "generativelanguage.googleapis.com", "gemini_api_key"},
}

// TestEachListCommandUsesOnlyItsProvidersKeyAndHost binds every key field to its own host, so Dex Web's broker never mixes them.
func TestEachListCommandUsesOnlyItsProvidersKeyAndHost(t *testing.T) {
	manifest := readStudioManifest(t, "connector.yaml")
	commands := map[string]studioCommand{}
	for _, command := range manifest.Spec.Studio.Commands {
		commands[command.ID] = command
	}
	require.Len(t, commands, len(providerListCommands), "every Studio command is a provider model list")
	for _, expected := range providerListCommands {
		command, isDeclared := commands[expected.commandID]
		require.True(t, isDeclared, expected.commandID)
		require.Equal(t, "llm.models-list", command.Capability)
		require.Equal(t, "GET", command.Request["method"])
		listURL, err := url.Parse(command.Request["url"].(string))
		require.NoError(t, err)
		require.Equal(t, "https", listURL.Scheme)
		require.Equal(t, expected.host, listURL.Host, expected.commandID)
		require.Equal(t, expected.credentialField, command.Request["credential"].(map[string]any)["field"], expected.commandID)
	}
	require.Equal(t, []string{"use.configuration.write", "llm.models-list"}, manifest.Spec.Studio.Setup.BackendCapabilities)
	require.Len(t, manifest.Spec.Studio.Units, 1)
	require.Equal(t, llmrouter.UIUnitModelPicker, manifest.Spec.Studio.Units[0].ID)
	require.Equal(t, []string{"llm.models-list"}, manifest.Spec.Studio.Units[0].BackendCapabilities)
}

// TestListCommandsMatchThePinnedProviderConnectors fails when a pinned provider changes its list request, so llm cannot drift.
func TestListCommandsMatchThePinnedProviderConnectors(t *testing.T) {
	manifest := readStudioManifest(t, "connector.yaml")
	for _, expected := range providerListCommands {
		t.Run(expected.commandID, func(t *testing.T) {
			providerManifest := readStudioManifest(t, filepath.Join(moduleDirectory(t, expected.providerModule), "connector.yaml"))
			providerCommand := findStudioCommand(t, providerManifest, expected.providerCommandID)
			llmCommand := findStudioCommand(t, manifest, expected.commandID)
			require.Equal(t, requestWithoutCredentialField(providerCommand.Request), requestWithoutCredentialField(llmCommand.Request))
		})
	}
}

// TestConnectionFieldsGuideTheUser keeps every visible field documented, and leaves provider endpoints out of the form.
func TestConnectionFieldsGuideTheUser(t *testing.T) {
	manifest := readStudioManifest(t, "connector.yaml")
	for _, field := range manifest.Spec.Configuration.Fields {
		require.NotEqual(t, "url", field.Type, "provider hosts are fixed, so no URL field is shown")
		require.NotContains(t, strings.ToLower(field.Name), "endpoint")
		require.NotEmpty(t, field.Description, field.Name)
	}
	require.True(t, manifest.Spec.Configuration.Fields[0].Required, "the connection model is required")
	require.Equal(t, "model", manifest.Spec.Configuration.Fields[0].Name)
	var keyFields []string
	for _, field := range manifest.Spec.Auth.Fields {
		require.Equal(t, "secretString", field.Type, field.Name)
		require.False(t, field.Required, "each key is optional, because a connection may use one provider")
		require.Contains(t, field.Description, "Every save replaces all three keys", field.Name)
		keyFields = append(keyFields, field.Name)
	}
	require.Equal(t, []string{"openai_api_key", "anthropic_api_key", "gemini_api_key"}, keyFields)
	guide := manifest.Spec.Auth.Guide
	require.Equal(t, "https://platform.openai.com/api-keys", guide.StartURL)
	require.Len(t, guide.Steps, 4)
	require.Contains(t, guide.Steps[1], "https://platform.claude.com/settings/keys")
	require.Contains(t, guide.Steps[2], "https://aistudio.google.com/api-keys")
	for _, providerDefault := range []int64{
		openai.DefaultConfig().MaxResponseBytes, claude.DefaultConfig().MaxResponseBytes, gemini.DefaultConfig().MaxResponseBytes,
	} {
		require.Equal(t, providerDefault, llmrouter.DefaultConfig().MaxResponseBytes, "maxResponseBytes keeps every provider's default")
	}
}

func readStudioManifest(t *testing.T, path string) studioManifest {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	var manifest studioManifest
	require.NoError(t, yaml.Unmarshal(contents, &manifest))
	return manifest
}

// moduleDirectory returns the source directory of the module version this module's build list selects.
func moduleDirectory(t *testing.T, modulePath string) string {
	t.Helper()
	output, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", modulePath).Output()
	require.NoError(t, err, "go list -m %s", modulePath)
	directory := strings.TrimSpace(string(output))
	require.NotEmpty(t, directory, "the %s module source is not downloaded", modulePath)
	return directory
}

func findStudioCommand(t *testing.T, manifest studioManifest, commandID string) studioCommand {
	t.Helper()
	for _, command := range manifest.Spec.Studio.Commands {
		if command.ID == commandID {
			return command
		}
	}
	t.Fatalf("the manifest declares no %s command", commandID)
	return studioCommand{}
}

func requestWithoutCredentialField(request map[string]any) map[string]any {
	copied := map[string]any{}
	for name, value := range request {
		copied[name] = value
	}
	credential := map[string]any{}
	for name, value := range request["credential"].(map[string]any) {
		if name != "field" {
			credential[name] = value
		}
	}
	copied["credential"] = credential
	return copied
}
