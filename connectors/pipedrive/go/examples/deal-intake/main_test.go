// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	dealintake "github.com/superdurable/dex-connectors-library/connectors/pipedrive/examples/deal-intake/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("PIPEDRIVE_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("PIPEDRIVE_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("PIPEDRIVE_EXAMPLE_MISSING", "fallback"))
}

func TestLoadSettingsReadsEveryPickAndTreatsAMissingOwnerAsBlank(t *testing.T) {
	// The key is built at run time, so no token-shaped 40-character literal is checked in.
	sourceFieldKey := strings.Repeat("ab", 20)
	dealPicks := map[string]any{"pipelineId": "1", "stageId": "3", "sourceObjectType": "deals", "sourceFieldKey": sourceFieldKey}
	settings, err := loadSettings(projectConfiguration(t, map[string]any{"ownerId": "7"}, dealPicks))
	require.NoError(t, err)
	require.Equal(t, dealintake.Settings{
		LeadOwner: dealintake.LeadOwnerConfiguration{OwnerID: "7"},
		Deal:      dealintake.DealConfiguration{PipelineID: "1", StageID: "3", SourceObjectType: "deals", SourceFieldKey: sourceFieldKey},
	}, settings)

	settings, err = loadSettings(projectConfiguration(t, nil, map[string]any{"pipelineId": "1", "stageId": "3"}))
	require.NoError(t, err)
	require.Empty(t, settings.LeadOwner.OwnerID)
	require.Empty(t, settings.Deal.SourceFieldKey)
}

func TestLoadSettingsRequiresTheDealStage(t *testing.T) {
	_, err := loadSettings(projectConfiguration(t, map[string]any{"ownerId": "7"}, nil))
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)
	require.ErrorContains(t, err, "deal stage")
}

// projectConfiguration holds the Steps' saved picks, as Dex Web saves them; a nil pick is not saved.
func projectConfiguration(t *testing.T, ownerConfiguration map[string]any, dealConfiguration map[string]any) projectconfig.Configuration {
	t.Helper()
	var configuration projectconfig.Configuration
	for _, entry := range []struct {
		configuration map[string]any
		reference     sdkgo.ConnectorConfigurationRef
	}{
		{ownerConfiguration, dealintake.LeadOwnerConfigurationRef()},
		{dealConfiguration, dealintake.DealConfigurationRef()},
	} {
		if entry.configuration == nil {
			continue
		}
		contents, err := json.Marshal(entry.configuration)
		require.NoError(t, err)
		configuration.OperationConfigurations = append(configuration.OperationConfigurations, projectconfig.OperationConfiguration{
			ConnectorID: entry.reference.ConnectorID, ConnectionName: entry.reference.ConnectionName,
			OperationID: entry.reference.OperationID, FlowType: entry.reference.FlowType, StepType: entry.reference.StepType,
			Configuration: contents,
		})
	}
	return configuration
}
