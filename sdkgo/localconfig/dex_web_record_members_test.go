// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package localconfig_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// writeDexWebConnections writes Dex Web-shaped records, including members a newer Dex Web might add.
func writeDexWebConnections(t *testing.T, path string, expiresAt time.Time) {
	t.Helper()
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{
			map[string]any{
				"connectorId": "gmail", "authMethodId": "google-oauth",
				"modulePath":    "github.com/superdurable/dex-connectors-library/connectors/google/gmail",
				"moduleVersion": "v0.14.0", "provider": "google", "connectionName": "sender",
				"configuration":       map[string]any{"endpoint": "https://gmail.example"},
				"credentials":         map[string]any{"access_token": "expired-token", "refresh_token": "refresh-token"},
				"credentialExpiresAt": expiresAt.UTC().Format(time.RFC3339),
				"futureMember":        map[string]any{"addedBy": "a newer Dex Web"},
			},
			map[string]any{
				"connectorId": "llm", "authMethodIds": []string{"openai", "anthropic"},
				"modulePath":    "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm",
				"moduleVersion": "v0.2.0", "provider": "llm", "connectionName": "default",
				"configuration": map[string]any{"model": "openai/model"},
				"credentials":   map[string]any{"access_token": "llm-token"},
			},
		},
		"triggerBindings": []any{map[string]any{
			"connectorId": "gmail", "connectionName": "sender", "triggerName": "messageReceived", "bindingName": "inbox",
			"configuration": map[string]any{"channelId": "INBOX"}, "futureBindingMember": true,
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
}

// readRecordMembers returns each record's raw members keyed by record identity.
func readRecordMembers(t *testing.T, path string) map[string]map[string]json.RawMessage {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	var file struct {
		Connections     []map[string]json.RawMessage `json:"connections"`
		TriggerBindings []map[string]json.RawMessage `json:"triggerBindings"`
	}
	require.NoError(t, json.Unmarshal(contents, &file))
	records := map[string]map[string]json.RawMessage{}
	for _, record := range file.Connections {
		records["connection "+string(record["connectorId"])+"/"+string(record["connectionName"])] = record
	}
	for _, record := range file.TriggerBindings {
		records["binding "+string(record["connectorId"])+"/"+string(record["bindingName"])] = record
	}
	return records
}

func TestLoadFileAcceptsDexWebRecordMembers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	writeDexWebConnections(t, path, time.Now().Add(time.Hour))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)

	var configuration testConfiguration
	require.NoError(t, store.DecodeConfiguration("gmail", "sender", &configuration))
	require.Equal(t, "https://gmail.example", configuration.Endpoint)
	credentials, err := localconfig.NewCredentialProvider(store, "llm", "default", decodeTestCredentials).
		Resolve(sdkgo.Call{Connection: sdkgo.ConnectionRef{Provider: "llm", Name: "default"}})
	require.NoError(t, err)
	require.Equal(t, "llm-token", credentials.AccessToken.Reveal())
}

func TestRefreshWritesDexWebRecordMembersBack(t *testing.T) {
	for _, test := range []struct {
		name                   string
		driver                 func(*atomic.Int32) testRefreshDriver
		expectedCredentialText string
	}{
		{
			name: "rotated credentials",
			driver: func(calls *atomic.Int32) testRefreshDriver {
				return testRefreshDriver{calls: calls, result: sdkgo.CredentialRefreshResult[testCredentials]{
					Credentials: testCredentials{
						AccessToken: sdkgo.NewSecretString("refreshed-token"), RefreshToken: sdkgo.NewSecretString("rotated-refresh-token"),
					},
					ExpiresAt: time.Now().Add(time.Hour).UTC(),
				}}
			},
			expectedCredentialText: "refreshed-token",
		},
		{
			name: "reauthorization required",
			driver: func(calls *atomic.Int32) testRefreshDriver {
				return testRefreshDriver{calls: calls, err: sdkgo.NewReauthorizationRequiredError(errors.New("invalid_grant"))}
			},
			expectedCredentialText: "expired-token",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "connections.json")
			writeDexWebConnections(t, path, time.Now().Add(-time.Minute))
			before := readRecordMembers(t, path)
			store, err := localconfig.LoadFile(path)
			require.NoError(t, err)
			provider := localconfig.NewRefreshingCredentialProvider(store, "gmail", "sender", decodeTestCredentials, encodeTestCredentials)
			calls := &atomic.Int32{}
			_, _ = sdkgo.ResolveCredential(
				context.Background(), provider, sdkgo.Call{Connection: sdkgo.ConnectionRef{Provider: "google", Name: "sender"}},
				test.driver(calls),
			)
			require.Equal(t, int32(1), calls.Load())

			after := readRecordMembers(t, path)
			require.Contains(t, string(after["connection \"gmail\"/\"sender\""]["credentials"]), test.expectedCredentialText)
			for identity, beforeMembers := range before {
				for name, beforeValue := range beforeMembers {
					switch name {
					case "credentials", "credentialExpiresAt", "credentialStatus":
						continue
					}
					require.JSONEq(t, string(beforeValue), string(after[identity][name]), "%s member %s", identity, name)
				}
			}
			_, err = localconfig.LoadFile(path)
			require.NoError(t, err, "the rewritten file must load again")
		})
	}
}

func TestLoadFileRejectsRecordMemberDifferingOnlyInCase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[{
		"connectorId":"gmail","ConnectorID":"other","modulePath":"m","moduleVersion":"v0.1.0","provider":"google",
		"connectionName":"sender","configuration":{},"credentials":{"access_token":"t"}}]}`), 0o600))
	_, err := localconfig.LoadFile(path)
	require.ErrorContains(t, err, `record member "ConnectorID" differs from "connectorId" only in case`)
}

func TestLoadOperationConfigurationAcceptsDexWebRecordMembers(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "connections.json")
	writeConnections(t, path, "https://example.test", "token", time.Now().Add(time.Hour))
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.UseConfigurationsSchemaVersion,
		"operationConfigurations": []any{map[string]any{
			"connectorId": "gmail", "connectionName": "sender", "operationId": "sendMessage",
			"flowType": "NotifyFlow", "stepType": "notify", "configuration": map[string]any{"channelId": "C123", "message": "hi"},
			"updatedBy": "a newer Dex Web",
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, localconfig.UseConfigurationsFileName), contents, 0o600))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	loaded, err := localconfig.LoadOperationConfiguration[testOperationConfiguration](store, sdkgo.ConnectorConfigurationRef{
		ConnectorID: "gmail", ConnectionName: "sender", OperationID: "sendMessage", FlowType: "NotifyFlow", StepType: "notify",
	})
	require.NoError(t, err)
	require.Equal(t, "C123", loaded.Value.ChannelID)
}
