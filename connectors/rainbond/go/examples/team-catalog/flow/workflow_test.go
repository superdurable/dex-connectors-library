// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teamcatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/rainbond/go"
	"github.com/superdurable/dex-connectors-library/connectors/rainbond/go/rainbondmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTeamCatalogFlowIdentityAndMapping(t *testing.T) {
	input := rainbond.ListTeamsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(rainbond.Connection{})))
	require.Equal(t, "StartListTeams", dex.GetFinalStepType[rainbond.ListTeamsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := rainbondmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
