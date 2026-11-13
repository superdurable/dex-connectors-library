// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teamcatalog

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/ones/go"
	"github.com/superdurable/dex-connectors-library/connectors/ones/go/onesmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestTeamCatalogFlowIdentityAndMapping(t *testing.T) {
	input := ones.ListTeamsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(ones.Connection{})))
	require.Equal(t, "StartListTeams", dex.GetFinalStepType[ones.ListTeamsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := onesmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
