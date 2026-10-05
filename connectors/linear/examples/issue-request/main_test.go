// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	issuerequest "github.com/superdurable/dex-connectors-library/connectors/linear/examples/issue-request/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("LINEAR_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("LINEAR_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("LINEAR_EXAMPLE_MISSING", "fallback"))
}

func TestConnectionOptionsRedirectOnlyWhenTheLocalVariableIsSet(t *testing.T) {
	t.Setenv(localAPIURLEnvironmentVariable, "")
	require.Empty(t, connectionOptions())
	t.Setenv(localAPIURLEnvironmentVariable, "http://127.0.0.1:8899/graphql")
	require.Len(t, connectionOptions(), 1)
}

func TestTeamSelectionIsOptionalAndLoadedFromTheProjectConfiguration(t *testing.T) {
	selection, err := loadTeamSelection(projectconfig.Configuration{})
	require.NoError(t, err)
	require.Equal(t, issuerequest.TeamSelection{}, selection)

	reference := issuerequest.TeamSelectionConfigurationRef()
	configuration := projectconfig.Configuration{OperationConfigurations: []projectconfig.OperationConfiguration{{
		ConnectorID: linear.ConnectorID, ConnectionName: reference.ConnectionName, OperationID: reference.OperationID,
		FlowType: reference.FlowType, StepType: reference.StepType,
		Configuration: json.RawMessage(`{"teamId":"2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4","teamKey":"ENG","teamName":"Engineering"}`),
	}}}
	selection, err = loadTeamSelection(configuration)
	require.NoError(t, err)
	require.Equal(t, issuerequest.TeamSelection{TeamID: "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4", TeamKey: "ENG", TeamName: "Engineering"}, selection)
}
