// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package localconfig_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

type testConfiguration struct {
	Endpoint string `json:"endpoint"`
}

type testCredentials struct {
	AccessToken sdkgo.SecretString
}

type testTriggerConfiguration struct {
	ChannelID string `json:"channelId"`
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
	credentials, err := provider.Resolve(sdkgo.Call{Connection: sdkgo.ConnectionRef{Provider: "google", Name: "sender"}})
	require.NoError(t, err)
	require.Equal(t, "token-one", credentials.AccessToken.Reveal())

	writeConnections(t, path, "https://two.example", "token-two", time.Now().Add(time.Hour))
	require.NoError(t, store.DecodeConfiguration("gmail", "sender", &configuration))
	require.Equal(t, "https://one.example", configuration.Endpoint)
	credentials, err = provider.Resolve(sdkgo.Call{Connection: sdkgo.ConnectionRef{Provider: "google", Name: "sender"}})
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
	_, err = provider.Resolve(sdkgo.Call{Connection: sdkgo.ConnectionRef{Provider: "google", Name: "sender"}})
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

func TestStoreDecodesTriggerBindingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	writeConnections(t, path, "https://example.test", "token", time.Now().Add(time.Hour))
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	var file map[string]any
	require.NoError(t, json.Unmarshal(contents, &file))
	file["triggerBindings"] = []any{map[string]any{
		"connectorId": "gmail", "connectionName": "sender", "triggerName": "messageCreated", "bindingName": "approval-start",
		"configuration": map[string]any{"channelId": "C123"},
	}}
	contents, err = json.Marshal(file)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))

	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	var configuration testTriggerConfiguration
	require.NoError(t, store.DecodeTriggerConfiguration("gmail", "sender", "messageCreated", "approval-start", &configuration))
	require.Equal(t, "C123", configuration.ChannelID)
	require.ErrorContains(t, store.DecodeTriggerConfiguration("gmail", "sender", "messageCreated", "missing", &configuration), "is not configured")
}

func TestDurableTriggerTargetReplaysEventAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	writeConnections(t, path, "https://example.test", "token", time.Now().Add(time.Hour))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	event := sdkgo.TriggerEvent[testTriggerConfiguration]{
		ID: "Ev-pending", OccurredAt: time.Unix(42, 0).UTC(), Payload: testTriggerConfiguration{ChannelID: "C123"},
	}
	firstTarget, err := localconfig.NewDurableTriggerTarget(
		store, "gmail", "sender", "messageCreated", "approval-start",
		sdkgo.TriggerTargetFunc[testTriggerConfiguration](func(context.Context, sdkgo.TriggerEvent[testTriggerConfiguration]) error {
			return nil
		}),
	)
	require.NoError(t, err)
	require.NoError(t, sdkgo.PrepareTriggerDelivery(context.Background(), firstTarget, event))

	var replayed []sdkgo.TriggerEvent[testTriggerConfiguration]
	restartedTarget, err := localconfig.NewDurableTriggerTarget(
		store, "gmail", "sender", "messageCreated", "approval-start",
		sdkgo.TriggerTargetFunc[testTriggerConfiguration](func(_ context.Context, received sdkgo.TriggerEvent[testTriggerConfiguration]) error {
			replayed = append(replayed, received)
			return nil
		}),
	)
	require.NoError(t, err)
	replayer, ok := restartedTarget.(sdkgo.TriggerDeliveryReplayer)
	require.True(t, ok)
	require.NoError(t, replayer.ReplayTriggerDeliveries(context.Background()))
	require.Equal(t, []sdkgo.TriggerEvent[testTriggerConfiguration]{event}, replayed)
	require.NoError(t, replayer.ReplayTriggerDeliveries(context.Background()))
	require.Len(t, replayed, 1)
}

func decodeTestCredentials(contents json.RawMessage) (testCredentials, error) {
	var raw struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(contents, &raw); err != nil {
		return testCredentials{}, err
	}
	return testCredentials{AccessToken: sdkgo.NewSecretString(raw.AccessToken)}, nil
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
