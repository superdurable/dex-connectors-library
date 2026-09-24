// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package localconfig_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex-connectors-library/sdk/go/localconfig"
)

type testConfiguration struct {
	Endpoint string `json:"endpoint"`
}

type testCredentials struct {
	AccessToken connector.SecretString
}

func TestStoreSnapshotsConfigurationAndReloadsCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	writeConnections(t, path, "https://one.example", "token-one", time.Now().Add(time.Hour))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)

	var configuration testConfiguration
	require.NoError(t, store.DecodeConfiguration("gmail", "sender", &configuration))
	require.Equal(t, "https://one.example", configuration.Endpoint)

	provider := localconfig.NewCredentialProvider(store, "gmail", "sender", decodeTestCredentials)
	credentials, err := provider.Resolve(connector.Call{Connection: connector.ConnectionRef{Provider: "google", Name: "sender"}})
	require.NoError(t, err)
	require.Equal(t, "token-one", credentials.AccessToken.Reveal())

	writeConnections(t, path, "https://two.example", "token-two", time.Now().Add(time.Hour))
	require.NoError(t, store.DecodeConfiguration("gmail", "sender", &configuration))
	require.Equal(t, "https://one.example", configuration.Endpoint)
	credentials, err = provider.Resolve(connector.Call{Connection: connector.ConnectionRef{Provider: "google", Name: "sender"}})
	require.NoError(t, err)
	require.Equal(t, "token-two", credentials.AccessToken.Reveal())
}

func TestLoadFromEnvironmentRejectsUnknownFieldsAndExpiredCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[],"unexpected":true}`), 0o600))
	t.Setenv(localconfig.EnvironmentVariable, path)
	_, err := localconfig.LoadFromEnvironment()
	require.ErrorContains(t, err, "unknown field")

	writeConnections(t, path, "https://example.test", "expired-token", time.Now().Add(-time.Minute))
	store, err := localconfig.LoadFromEnvironment()
	require.NoError(t, err)
	provider := localconfig.NewCredentialProvider(store, "gmail", "sender", decodeTestCredentials)
	_, err = provider.Resolve(connector.Call{Connection: connector.ConnectionRef{Provider: "google", Name: "sender"}})
	require.ErrorContains(t, err, "credentials are expired")
}

func TestLoadFileRejectsSymlinkAndDuplicateConnection(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.json")
	writeConnections(t, target, "https://example.test", "token", time.Now().Add(time.Hour))
	symlink := filepath.Join(directory, "connections.json")
	require.NoError(t, os.Symlink(target, symlink))
	_, err := localconfig.LoadFile(symlink)
	require.ErrorContains(t, err, "regular file")

	contents, err := os.ReadFile(target)
	require.NoError(t, err)
	var file map[string]any
	require.NoError(t, json.Unmarshal(contents, &file))
	connections := file["connections"].([]any)
	file["connections"] = append(connections, connections[0])
	contents, err = json.Marshal(file)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(target, contents, 0o600))
	_, err = localconfig.LoadFile(target)
	require.ErrorContains(t, err, "is duplicated")
}

func TestLoadFileRejectsCredentialFileWithBroadPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	writeConnections(t, path, "https://example.test", "token", time.Now().Add(time.Hour))
	require.NoError(t, os.Chmod(path, 0o644))

	_, err := localconfig.LoadFile(path)
	require.ErrorContains(t, err, "permissions must be 0600")
}

func decodeTestCredentials(contents json.RawMessage) (testCredentials, error) {
	var raw struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(contents, &raw); err != nil {
		return testCredentials{}, err
	}
	return testCredentials{AccessToken: connector.NewSecretString(raw.AccessToken)}, nil
}

func writeConnections(t *testing.T, path string, endpoint string, token string, expiresAt time.Time) {
	t.Helper()
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": "gmail", "modulePath": "github.com/superdurable/dex-connectors-library/connectors/google/gmail",
			"moduleVersion": "v0.1.1", "provider": "google", "connectionName": "sender",
			"configuration": map[string]any{"endpoint": endpoint}, "credentials": map[string]any{"access_token": token},
			"credentialExpiresAt": expiresAt.UTC().Format(time.RFC3339),
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
}
