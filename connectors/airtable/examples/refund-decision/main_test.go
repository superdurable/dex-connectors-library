// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	refunddecision "github.com/superdurable/dex-connectors-library/connectors/airtable/examples/refund-decision/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
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
	settings, err := loadSettings(projectConfiguration(t, policyTablePick, logTablePick))
	require.NoError(t, err)
	require.Equal(t, refunddecision.Settings{
		PolicyTable: refunddecision.TableSelection{BaseID: "appRefundBase0001", BaseName: "Refunds", TableID: "tblPolicies000001", TableName: "Policies"},
		LogTable:    refunddecision.TableSelection{BaseID: "appRefundBase0001", BaseName: "Refunds", TableID: "tblDecisionLog001", TableName: "Decision Log"},
	}, settings)
}

func TestLoadSettingsRequiresBothTablePicks(t *testing.T) {
	_, err := loadSettings(projectConfiguration(t, policyTablePick, nil))
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)
	require.ErrorContains(t, err, "decision log table")
	_, err = loadSettings(projectConfiguration(t, nil, logTablePick))
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)
	require.ErrorContains(t, err, "policy table")
}

// projectConfiguration is the project configuration Dex Web saves: the connection and the given table picks.
func projectConfiguration(t *testing.T, policyConfiguration map[string]any, logConfiguration map[string]any) projectconfig.Configuration {
	t.Helper()
	configuration := projectconfig.Configuration{Connections: []projectconfig.ConnectionConfiguration{{
		ConnectorID: "airtable", ConnectionName: refunddecision.ConnectionName,
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/airtable", Provider: "airtable",
		Configuration: json.RawMessage(`{}`),
	}}}
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
		contents, err := json.Marshal(entry.configuration)
		require.NoError(t, err)
		configuration.OperationConfigurations = append(configuration.OperationConfigurations, projectconfig.OperationConfiguration{
			ConnectorID: entry.reference.ConnectorID, ConnectionName: entry.reference.ConnectionName, OperationID: entry.reference.OperationID,
			FlowType: entry.reference.FlowType, StepType: entry.reference.StepType, Configuration: contents,
		})
	}
	return configuration
}
