// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/alibaba/qwen"
	summarizetext "github.com/superdurable/dex-connectors-library/connectors/alibaba/qwen/examples/summarize-text/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// TestSummaryModelComesFromTheDexWebStepPick reads the SummarizeText Step's
// pick from the use-configuration file that Dex Web Connections writes.
func TestSummaryModelComesFromTheDexWebStepPick(t *testing.T) {
	picked, err := loadSummaryModelConfiguration(writeStore(t, `{"model":"qwen3.8-flash"}`))
	require.NoError(t, err)
	require.Equal(t, "qwen3.8-flash", picked.Model)

	inherited, err := loadSummaryModelConfiguration(writeStore(t, `{"model":""}`))
	require.NoError(t, err)
	require.Empty(t, inherited.Model, "an empty pick keeps the connection's model")

	neverConfigured, err := loadSummaryModelConfiguration(writeStore(t, ""))
	require.NoError(t, err, "a Step never configured in Dex Web uses the connection's model")
	require.Empty(t, neverConfigured.Model)

	_, err = loadSummaryModelConfiguration(writeStore(t, `{"model":"qwen3.8-flash","temperature":1}`))
	require.Error(t, err, "an unknown field is a configuration error, not a missing pick")
}

// TestLocalConnectionAcceptsOnlyADashScopeEndpoint builds the connection the Worker builds at startup.
func TestLocalConnectionAcceptsOnlyADashScopeEndpoint(t *testing.T) {
	_, err := qwen.NewLocalConnection(writeStoreWithConfiguration(t, map[string]any{
		"endpoint": "https://cn-hongkong.dashscope.aliyuncs.com/compatible-mode/v1",
	}, ""), summarizetext.ConnectionName)
	require.NoError(t, err)
	_, err = qwen.NewLocalConnection(writeStoreWithConfiguration(t, map[string]any{
		"endpoint": "https://llm-abc123.ap-southeast-1.maas.aliyuncs.com/compatible-mode/v1",
	}, ""), summarizetext.ConnectionName)
	require.ErrorContains(t, err, "DashScope endpoint", "a workspace-dedicated endpoint is out of scope")
}

// writeStore writes a Qwen connection and, when stepConfiguration is set, the SummarizeText Step's saved pick.
func writeStore(t *testing.T, stepConfiguration string) *localconfig.Store {
	t.Helper()
	return writeStoreWithConfiguration(t, map[string]any{}, stepConfiguration)
}

func writeStoreWithConfiguration(t *testing.T, configuration map[string]any, stepConfiguration string) *localconfig.Store {
	t.Helper()
	directory := t.TempDir()
	connections, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": qwen.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/alibaba/qwen",
			"moduleVersion": "v0.1.0", "provider": "alibaba", "connectionName": summarizetext.ConnectionName,
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
