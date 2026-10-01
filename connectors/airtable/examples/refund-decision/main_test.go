// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	refunddecision "github.com/superdurable/dex-connectors-library/connectors/airtable/examples/refund-decision/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

var (
	policyTablePick = map[string]any{"baseId": "appRefundBase0001", "baseName": "Refunds", "tableId": "tblPolicies000001", "tableName": "Policies"}
	logTablePick    = map[string]any{"baseId": "appRefundBase0001", "baseName": "Refunds", "tableId": "tblDecisionLog001", "tableName": "Decision Log"}
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("AIRTABLE_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("AIRTABLE_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("AIRTABLE_EXAMPLE_MISSING", "fallback"))
}

func TestNewLoggerUsesInfoForAnUnknownLevel(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(&output, "verbose")
	logger.Debug("hidden")
	require.Contains(t, output.String(), "LOG_LEVEL is not debug, info, warn, or error")
	require.NotContains(t, output.String(), "hidden")
}

func TestLoadSettingsReadsBothTablePicks(t *testing.T) {
	settings, err := loadSettings(writeLocalConfiguration(t, policyTablePick, logTablePick))
	require.NoError(t, err)
	require.Equal(t, refunddecision.Settings{
		PolicyTable: refunddecision.TableSelection{BaseID: "appRefundBase0001", BaseName: "Refunds", TableID: "tblPolicies000001", TableName: "Policies"},
		LogTable:    refunddecision.TableSelection{BaseID: "appRefundBase0001", BaseName: "Refunds", TableID: "tblDecisionLog001", TableName: "Decision Log"},
	}, settings)
}

func TestLoadSettingsRequiresBothTablePicks(t *testing.T) {
	_, err := loadSettings(writeLocalConfiguration(t, policyTablePick, nil))
	require.ErrorIs(t, err, localconfig.ErrConfigurationNotFound)
	require.ErrorContains(t, err, "decision log table")
	_, err = loadSettings(writeLocalConfiguration(t, nil, logTablePick))
	require.ErrorIs(t, err, localconfig.ErrConfigurationNotFound)
	require.ErrorContains(t, err, "policy table")
}

func writeLocalConfiguration(t *testing.T, policyConfiguration map[string]any, logConfiguration map[string]any) *localconfig.Store {
	t.Helper()
	directory := t.TempDir()
	writeJSONFile(t, filepath.Join(directory, "connections.json"), map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []map[string]any{{
			"connectorId": "airtable", "modulePath": "github.com/superdurable/dex-connectors-library/connectors/airtable",
			"moduleVersion": "v0.1.0", "provider": "airtable", "connectionName": refunddecision.ConnectionName,
			"configuration": map[string]any{},
			"credentials":   map[string]any{"personal_access_token": "patTESTtoken.0123456789abcdef"},
		}},
	})
	records := []map[string]any{}
	for _, entry := range []struct {
		configuration map[string]any
		reference     sdkgo.ConnectorConfigurationRef
	}{
		{policyConfiguration, refunddecision.PolicyTableConfigurationRef()},
		{logConfiguration, refunddecision.LogTableConfigurationRef()},
	} {
		if entry.configuration == nil {
			continue
		}
		record := referenceRecord(t, entry.reference)
		record["configuration"] = entry.configuration
		records = append(records, record)
	}
	writeJSONFile(t, filepath.Join(directory, localconfig.UseConfigurationsFileName), map[string]any{
		"schemaVersion": localconfig.UseConfigurationsSchemaVersion, "operationConfigurations": records,
	})
	store, err := localconfig.LoadFile(filepath.Join(directory, "connections.json"))
	require.NoError(t, err)
	return store
}

func referenceRecord(t *testing.T, reference sdkgo.ConnectorConfigurationRef) map[string]any {
	t.Helper()
	contents, err := json.Marshal(reference)
	require.NoError(t, err)
	var record map[string]any
	require.NoError(t, json.Unmarshal(contents, &record))
	return record
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	contents, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
}
