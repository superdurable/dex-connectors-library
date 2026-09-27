// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/moonshot/kimi"
	summarizetext "github.com/superdurable/dex-connectors-library/connectors/moonshot/kimi/examples/summarize-text/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// TestSummaryModelComesFromTheDexWebStepPick reads the SummarizeText Step's
// pick from the use-configuration file that Dex Web Connections writes.
func TestSummaryModelComesFromTheDexWebStepPick(t *testing.T) {
	picked, err := loadSummaryModelConfiguration(writeStore(t, map[string]any{}, `{"model":"kimi-k3"}`))
	require.NoError(t, err)
	require.Equal(t, "kimi-k3", picked.Model)

	inherited, err := loadSummaryModelConfiguration(writeStore(t, map[string]any{}, `{"model":""}`))
	require.NoError(t, err)
	require.Empty(t, inherited.Model, "an empty pick keeps the connection's model")

	neverConfigured, err := loadSummaryModelConfiguration(writeStore(t, map[string]any{}, ""))
	require.NoError(t, err, "a Step never configured in Dex Web uses the connection's model")
	require.Empty(t, neverConfigured.Model)

	_, err = loadSummaryModelConfiguration(writeStore(t, map[string]any{}, `{"model":"kimi-k3","temperature":1}`))
	require.Error(t, err, "an unknown field is a configuration error, not a missing pick")
}

// TestLocalConnectionAcceptsOnlyTheTwoKimiPlatforms loads the endpoint that the Dex Web connection form saves.
func TestLocalConnectionAcceptsOnlyTheTwoKimiPlatforms(t *testing.T) {
	for _, endpoint := range []string{"https://api.moonshot.ai/v1", "https://api.moonshot.cn/v1"} {
		_, err := kimi.NewLocalConnection(writeStore(t, map[string]any{"endpoint": endpoint}, ""), summarizetext.ConnectionName)
		require.NoError(t, err, endpoint)
	}
	_, err := kimi.NewLocalConnection(writeStore(t, map[string]any{"endpoint": "https://kimi-proxy.example/v1"}, ""), summarizetext.ConnectionName)
	require.ErrorContains(t, err, "endpoint")
}

// writeStore writes a Kimi connection with configuration and, when stepConfiguration is set, the SummarizeText Step's saved pick.
func writeStore(t *testing.T, configuration map[string]any, stepConfiguration string) *localconfig.Store {
	t.Helper()
	directory := t.TempDir()
	connections, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": kimi.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/moonshot/kimi",
			"moduleVersion": "v0.1.0", "provider": "moonshot", "connectionName": summarizetext.ConnectionName,
			"configuration": configuration, "credentials": map[string]any{"api_key": "sk-SENTINEL-step-pick"},
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
