// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	grok "github.com/superdurable/dex-connectors-library/connectors/xai"
	summarizetext "github.com/superdurable/dex-connectors-library/connectors/xai/examples/summarize-text/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// TestSummaryModelComesFromTheDexWebStepPick reads the SummarizeText Step's
// pick from the use-configuration file that Dex Web Connections writes.
func TestSummaryModelComesFromTheDexWebStepPick(t *testing.T) {
	picked, err := loadSummaryModelConfiguration(writeStore(t, `{"model":"grok-4.7"}`))
	require.NoError(t, err)
	require.Equal(t, "grok-4.7", picked.Model)

	inherited, err := loadSummaryModelConfiguration(writeStore(t, `{"model":""}`))
	require.NoError(t, err)
	require.Empty(t, inherited.Model, "an empty pick keeps the connection's model")

	neverConfigured, err := loadSummaryModelConfiguration(writeStore(t, ""))
	require.NoError(t, err, "a Step never configured in Dex Web uses the connection's model")
	require.Empty(t, neverConfigured.Model)

	_, err = loadSummaryModelConfiguration(writeStore(t, `{"model":"grok-4.7","temperature":1}`))
	require.Error(t, err, "an unknown field is a configuration error, not a missing pick")
}

// TestLocalConnectionAcceptsOnlyXAIEndpoints loads the connection the way the Worker does.
func TestLocalConnectionAcceptsOnlyXAIEndpoints(t *testing.T) {
	for _, endpoint := range []string{"", "https://api.x.ai/v1", "https://us.api.x.ai/v1"} {
		_, err := grok.NewLocalConnection(writeStoreWithEndpoint(t, endpoint, ""), summarizetext.ConnectionName)
		require.NoError(t, err, endpoint)
	}
	_, err := grok.NewLocalConnection(writeStoreWithEndpoint(t, "https://attacker.example/v1", ""), summarizetext.ConnectionName)
	require.ErrorContains(t, err, "endpoint must be")
}

// writeStore writes a Grok connection and, when stepConfiguration is set, the SummarizeText Step's saved pick.
func writeStore(t *testing.T, stepConfiguration string) *localconfig.Store {
	return writeStoreWithEndpoint(t, "", stepConfiguration)
}

func writeStoreWithEndpoint(t *testing.T, endpoint string, stepConfiguration string) *localconfig.Store {
	t.Helper()
	directory := t.TempDir()
	configuration := map[string]any{}
	if endpoint != "" {
		configuration["endpoint"] = endpoint
	}
	connections, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": grok.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/xai",
			"moduleVersion": "v0.1.0", "provider": "xai", "connectionName": summarizetext.ConnectionName,
			"configuration": configuration, "credentials": map[string]any{"api_key": "xai-SENTINEL-step-pick"},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "connections.json"), connections, 0o600))
	if stepConfiguration != "" {
		reference := summarizetext.SummaryModelConfigurationRef()
		useConfigurations, err := json.Marshal(map[string]any{
			"schemaVersion": localconfig.UseConfigurationsSchemaVersion,
			"operationConfigurations": []any{map[string]any{
				"connectorId": reference.ConnectorID, "connectionName": reference.ConnectionName, "operationId": reference.OperationID,
				"flowType": reference.FlowType, "stepType": reference.StepType, "configuration": json.RawMessage(stepConfiguration),
			}},
		})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(directory, localconfig.UseConfigurationsFileName), useConfigurations, 0o600))
	}
	store, err := localconfig.LoadFile(filepath.Join(directory, "connections.json"))
	require.NoError(t, err)
	return store
}
