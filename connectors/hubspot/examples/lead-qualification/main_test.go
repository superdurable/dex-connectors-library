// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	leadqualification "github.com/superdurable/dex-connectors-library/connectors/hubspot/examples/lead-qualification/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("HUBSPOT_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("HUBSPOT_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("HUBSPOT_EXAMPLE_MISSING", "fallback"))
}

func TestLoadSettingsReadsBothPicksAndTreatsAMissingOwnerAsBlank(t *testing.T) {
	configuration := projectConfiguration(t, map[string]any{
		"ownerId": "77",
	}, map[string]any{"pipelineId": "default", "stageId": "qualifiedtobuy"})
	settings, err := loadSettings(configuration)
	require.NoError(t, err)
	require.Equal(t, leadqualification.Settings{
		LeadOwner:          leadqualification.LeadOwnerConfiguration{OwnerID: "77"},
		QualifiedDealStage: leadqualification.QualifiedDealStageConfiguration{PipelineID: "default", StageID: "qualifiedtobuy"},
	}, settings)

	withoutOwner := projectConfiguration(t, nil, map[string]any{"pipelineId": "default", "stageId": "qualifiedtobuy"})
	settings, err = loadSettings(withoutOwner)
	require.NoError(t, err)
	require.Empty(t, settings.LeadOwner.OwnerID)
}

func TestLoadSettingsRequiresTheQualifiedDealStage(t *testing.T) {
	_, err := loadSettings(projectConfiguration(t, map[string]any{"ownerId": "77"}, nil))
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)
	require.ErrorContains(t, err, "qualified deal stage")
}

// projectConfiguration holds the Steps' saved picks, as Dex Web saves them; a nil pick is not saved.
func projectConfiguration(t *testing.T, ownerConfiguration map[string]any, stageConfiguration map[string]any) projectconfig.Configuration {
	t.Helper()
	var configuration projectconfig.Configuration
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
