// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package multipleauthselection

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

func TestDecodeLocalCredentialsKeepsSelectedAuthMethodsInAddOrder(t *testing.T) {
	credentials, err := decodeLocalCredentials(json.RawMessage(`{
		"auth_methods": ["anthropic", "openai"],
		"anthropic_api_key": "anthropic-test-key",
		"openai_api_key": "openai-test-key"
	}`))

	require.NoError(t, err)
	require.Equal(t, []string{"anthropic", "openai"}, credentials.AuthMethodIDs)
	require.True(t, credentials.HasAuthMethod("anthropic"))
	require.True(t, credentials.HasAuthMethod("openai"))
	require.False(t, credentials.HasAuthMethod("gemini"))
	require.False(t, credentials.HasAuthMethod(""))
	require.Equal(t, "anthropic-test-key", credentials.AnthropicAPIKey.Reveal())
	require.Equal(t, "openai-test-key", credentials.OpenAIAPIKey.Reveal())
	require.Empty(t, credentials.GeminiAPIKey.Reveal())
}

func TestDecodeLocalCredentialsRejectsInvalidSelections(t *testing.T) {
	testCases := map[string]struct {
		contents string
		message  string
	}{
		"missing selection":         {`{"openai_api_key": "openai-test-key"}`, "credential auth_methods is required"},
		"empty selection":           {`{"auth_methods": [], "openai_api_key": "openai-test-key"}`, "credential auth_methods is required"},
		"duplicate method":          {`{"auth_methods": ["openai", "openai"], "openai_api_key": "openai-test-key"}`, "credential auth_methods must be unique"},
		"undeclared method":         {`{"auth_methods": ["openai", "mistral"], "openai_api_key": "openai-test-key"}`, "credential auth_methods contains an undeclared auth method"},
		"selected key missing":      {`{"auth_methods": ["openai", "gemini"], "openai_api_key": "openai-test-key"}`, "credential gemini_api_key is required"},
		"single selection wire key": {`{"auth_method": "openai", "openai_api_key": "openai-test-key"}`, `"auth_method"`},
		"selection is not a list":   {`{"auth_methods": "openai", "openai_api_key": "openai-test-key"}`, "auth_methods"},
	}
	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			_, err := decodeLocalCredentials(json.RawMessage(testCase.contents))
			require.ErrorContains(t, err, testCase.message)
			require.NotContains(t, err.Error(), "openai-test-key")
		})
	}
}

func TestCredentialsValidateChecksOnlySelectedMethods(t *testing.T) {
	require.NoError(t, Credentials{AuthMethodIDs: []string{"gemini"}, GeminiAPIKey: sdkgo.NewSecretString("gemini-test-key")}.Validate())
	require.ErrorContains(t, Credentials{AuthMethodIDs: []string{"gemini", "anthropic"}, GeminiAPIKey: sdkgo.NewSecretString("gemini-test-key")}.Validate(), "credential anthropic_api_key is required")
	require.ErrorContains(t, Credentials{}.Validate(), "credential auth_methods is required")
	require.False(t, Credentials{}.HasAuthMethod("openai"))
}

func TestConfigTypeChecksButNeverRequiresMethodConfiguration(t *testing.T) {
	require.NoError(t, Config{}.Validate(), "openaiProjectId is required only while openai is selected, which Config cannot see")
	require.Equal(t, GeminiAPIVersionV1beta, DefaultConfig().GeminiAPIVersion)
	require.Equal(t, GeminiAPIVersionV1beta, withConfigDefaults(Config{}).GeminiAPIVersion)
	require.Equal(t, GeminiAPIVersionV1, withConfigDefaults(Config{GeminiAPIVersion: GeminiAPIVersionV1}).GeminiAPIVersion)
	require.ErrorContains(t, Config{GeminiAPIVersion: "v2"}.Validate(), "configuration geminiApiVersion is invalid")
}

func TestNewLocalConnectionDecodesMethodConfigurationAndSelectedCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
		"connections": [{
			"connectorId": "multiple-auth-selection-fixture",
			"modulePath": "example.com/multipleauthselection",
			"moduleVersion": "v0.1.0",
			"provider": "example",
			"connectionName": "providers",
			"configuration": {"model": "anthropic/claude-sonnet-5", "anthropicWorkspaceId": "wrkspc_test"},
			"credentials": {"auth_methods": ["anthropic"], "anthropic_api_key": "anthropic-test-key"}
		}]
	}`), 0o600))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)

	connection, err := NewLocalConnection(store, "providers")

	require.NoError(t, err)
	require.Equal(t, Config{
		Model: "anthropic/claude-sonnet-5", AnthropicWorkspaceID: "wrkspc_test", GeminiAPIVersion: GeminiAPIVersionV1beta,
	}, connection.client.config)
	credentials, err := connection.client.credentials.Resolve(sdkgo.Call{Connection: sdkgo.ConnectionRef{Provider: "example", Name: "providers"}})
	require.NoError(t, err)
	require.Equal(t, []string{"anthropic"}, credentials.AuthMethodIDs)
	require.Equal(t, "anthropic-test-key", credentials.AnthropicAPIKey.Reveal())
}

func TestNewLocalConnectionRejectsUnknownConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
		"connections": [{
			"connectorId": "multiple-auth-selection-fixture",
			"modulePath": "example.com/multipleauthselection",
			"moduleVersion": "v0.1.0",
			"provider": "example",
			"connectionName": "providers",
			"configuration": {"mistralEndpoint": "https://mistral.example"},
			"credentials": {"auth_methods": ["anthropic"], "anthropic_api_key": "anthropic-test-key"}
		}]
	}`), 0o600))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)

	_, err = NewLocalConnection(store, "providers")

	require.ErrorContains(t, err, "mistralEndpoint")
}
