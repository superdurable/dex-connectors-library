// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	llmrouter "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm"
	summarizetext "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm/examples/summarize-text/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// TestSummaryModelComesFromTheDexWebStepPick reads the SummarizeText Step's
// pick from the use-configuration file that Dex Web Connections writes.
func TestSummaryModelComesFromTheDexWebStepPick(t *testing.T) {
	picked, err := loadSummaryModelConfiguration(writeStore(t, `{"model":"gemini/gemini-3.8-flash"}`))
	require.NoError(t, err)
	require.Equal(t, "gemini/gemini-3.8-flash", picked.Model)

	inherited, err := loadSummaryModelConfiguration(writeStore(t, `{"model":""}`))
	require.NoError(t, err)
	require.Empty(t, inherited.Model, "an empty pick keeps the connection default")

	neverConfigured, err := loadSummaryModelConfiguration(writeStore(t, ""))
	require.NoError(t, err, "a Step never configured in Dex Web uses the connection default")
	require.Empty(t, neverConfigured.Model)

	_, err = loadSummaryModelConfiguration(writeStore(t, `{"model":"openai","temperature":1}`))
	require.Error(t, err, "an unknown field is a configuration error, not a missing pick")
}

// TestLocalConnectionTakesAnOptionalDefaultModel builds the connection the Worker uses from Dex Web's files.
func TestLocalConnectionTakesAnOptionalDefaultModel(t *testing.T) {
	_, err := llmrouter.NewLocalConnection(writeStoreWithConfiguration(t, map[string]any{"model": "anthropic/claude-sonnet-5"}, ""),
		summarizetext.ConnectionName)
	require.NoError(t, err)

	_, err = llmrouter.NewLocalConnection(writeStoreWithConfiguration(t, map[string]any{}, ""), summarizetext.ConnectionName)
	require.NoError(t, err, "a connection saved without a default model uses the first added provider's default")

	_, err = llmrouter.NewLocalConnection(writeStoreWithConfiguration(t, map[string]any{"model": "claude-sonnet-5"}, ""),
		summarizetext.ConnectionName)
	require.ErrorContains(t, err, "provider/model", "a default model without its provider fails at startup")
}

// writeStore writes an llm connection and, when stepConfiguration is set, the SummarizeText Step's saved pick.
func writeStore(t *testing.T, stepConfiguration string) *localconfig.Store {
	t.Helper()
	return writeStoreWithConfiguration(t, map[string]any{"model": "openai"}, stepConfiguration)
}

func writeStoreWithConfiguration(t *testing.T, configuration map[string]any, stepConfiguration string) *localconfig.Store {
	t.Helper()
	directory := t.TempDir()
	connections, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": llmrouter.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/superdurable/llm",
			"moduleVersion": "v0.2.0", "provider": "llm", "connectionName": summarizetext.ConnectionName,
			"configuration": configuration,
			"credentials":   map[string]any{"auth_methods": []string{"openai"}, "openai_api_key": "SENTINEL-llm-step-pick"},
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
