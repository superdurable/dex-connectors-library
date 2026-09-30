// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	leadqualification "github.com/superdurable/dex-connectors-library/connectors/hubspot/examples/lead-qualification/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("HUBSPOT_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("HUBSPOT_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("HUBSPOT_EXAMPLE_MISSING", "fallback"))
}

func TestLoadSettingsReadsBothPicksAndTreatsAMissingOwnerAsBlank(t *testing.T) {
	store := writeLocalConfiguration(t, map[string]any{
		"ownerId": "77",
	}, map[string]any{"pipelineId": "default", "stageId": "qualifiedtobuy"})
	settings, err := loadSettings(store)
	require.NoError(t, err)
	require.Equal(t, leadqualification.Settings{
		LeadOwner:          leadqualification.LeadOwnerConfiguration{OwnerID: "77"},
		QualifiedDealStage: leadqualification.QualifiedDealStageConfiguration{PipelineID: "default", StageID: "qualifiedtobuy"},
	}, settings)

	withoutOwner := writeLocalConfiguration(t, nil, map[string]any{"pipelineId": "default", "stageId": "qualifiedtobuy"})
	settings, err = loadSettings(withoutOwner)
	require.NoError(t, err)
	require.Empty(t, settings.LeadOwner.OwnerID)
}

func TestLoadSettingsRequiresTheQualifiedDealStage(t *testing.T) {
	_, err := loadSettings(writeLocalConfiguration(t, map[string]any{"ownerId": "77"}, nil))
	require.ErrorIs(t, err, localconfig.ErrConfigurationNotFound)
	require.ErrorContains(t, err, "qualified deal stage")
}

func writeLocalConfiguration(t *testing.T, ownerConfiguration map[string]any, stageConfiguration map[string]any) *localconfig.Store {
	t.Helper()
	directory := t.TempDir()
	writeJSONFile(t, filepath.Join(directory, "connections.json"), map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []map[string]any{{
			"connectorId": "hubspot", "modulePath": "github.com/superdurable/dex-connectors-library/connectors/hubspot",
			"moduleVersion": "v0.1.0", "provider": "hubspot", "connectionName": leadqualification.ConnectionName,
			"configuration": map[string]any{},
			"credentials":   map[string]any{"auth_method": "private-app-token", "access_token": "pat-na1-test-token"},
		}},
	})
	var records []map[string]any
	for _, entry := range []struct {
		configuration map[string]any
		reference     sdkgo.ConnectorConfigurationRef
	}{
		{ownerConfiguration, leadqualification.LeadOwnerConfigurationRef()},
		{stageConfiguration, leadqualification.QualifiedDealStageConfigurationRef()},
	} {
		if entry.configuration == nil {
			continue
		}
		record := referenceRecord(t, entry.reference)
		record["configuration"] = entry.configuration
		records = append(records, record)
	}
	if records == nil {
		records = []map[string]any{}
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
