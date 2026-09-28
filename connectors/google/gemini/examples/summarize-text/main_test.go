// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	summarizetext "github.com/superdurable/dex-connectors-library/connectors/google/gemini/examples/summarize-text/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// TestSummaryModelComesFromTheDexWebStepPick reads the SummarizeText Step's
// pick from the use-configuration file that Dex Web Connections writes.
func TestSummaryModelComesFromTheDexWebStepPick(t *testing.T) {
	picked, err := loadSummaryModelConfiguration(writeStore(t, `{"model":"gemini-3.8-flash"}`))
	require.NoError(t, err)
	require.Equal(t, "gemini-3.8-flash", picked.Model)

	inherited, err := loadSummaryModelConfiguration(writeStore(t, `{"model":""}`))
	require.NoError(t, err)
	require.Empty(t, inherited.Model, "an empty pick keeps the connection's model")

	neverConfigured, err := loadSummaryModelConfiguration(writeStore(t, ""))
	require.NoError(t, err, "a Step never configured in Dex Web uses the connection's model")
	require.Empty(t, neverConfigured.Model)

	_, err = loadSummaryModelConfiguration(writeStore(t, `{"model":"gemini-3.8-flash","temperature":1}`))
	require.Error(t, err, "an unknown field is a configuration error, not a missing pick")
}

func TestRunRequiresTheLocalConnectionFile(t *testing.T) {
	t.Setenv(localconfig.EnvironmentVariable, filepath.Join(t.TempDir(), "missing.json"))
	require.Error(t, run(t.Context()))
}

// writeStore writes a Gemini connection and, when stepConfiguration is set, the SummarizeText Step's saved pick.
func writeStore(t *testing.T, stepConfiguration string) *localconfig.Store {
	t.Helper()
	directory := t.TempDir()
	connections, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": gemini.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/google/gemini",
			"moduleVersion": "v0.3.0", "provider": "google", "connectionName": summarizetext.ConnectionName,
			"configuration": map[string]any{}, "credentials": map[string]any{"api_key": "AIzaSENTINEL-step-pick"},
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
