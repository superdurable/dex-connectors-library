// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectcatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/jihugitlab/go"
	"github.com/superdurable/dex-connectors-library/connectors/jihugitlab/go/jihugitlabmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestProjectCatalogFlowIdentityAndMapping(t *testing.T) {
	input := jihugitlab.ListMemberProjectsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(jihugitlab.Connection{})))
	require.Equal(t, "StartListMemberProjects", dex.GetFinalStepType[jihugitlab.ListMemberProjectsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := jihugitlabmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
