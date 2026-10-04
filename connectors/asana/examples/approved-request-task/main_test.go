// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	approvedrequesttask "github.com/superdurable/dex-connectors-library/connectors/asana/examples/approved-request-task/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("ASANA_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("ASANA_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("ASANA_EXAMPLE_MISSING", "fallback"))
}

func TestProjectSelectionIsOptionalAndLoadedFromTheProjectConfiguration(t *testing.T) {
	selection, err := loadProjectSelection(projectconfig.Configuration{})
	require.NoError(t, err)
	require.Equal(t, approvedrequesttask.ProjectSelection{}, selection)

	reference := approvedrequesttask.ProjectSelectionConfigurationRef()
	configuration := projectconfig.Configuration{OperationConfigurations: []projectconfig.OperationConfiguration{{
		ConnectorID: reference.ConnectorID, ConnectionName: reference.ConnectionName, OperationID: reference.OperationID,
		FlowType: reference.FlowType, StepType: reference.StepType,
		Configuration: json.RawMessage(`{"workspaceId":"1100000000000001","projectId":"1201000000000001","projectName":"Facilities","sectionId":"1201000000000101","sectionName":"Approved"}`),
	}}}
	selection, err = loadProjectSelection(configuration)
	require.NoError(t, err)
	require.Equal(t, approvedrequesttask.ProjectSelection{
		WorkspaceID: "1100000000000001", ProjectID: "1201000000000001", ProjectName: "Facilities",
		SectionID: "1201000000000101", SectionName: "Approved",
	}, selection)
}
