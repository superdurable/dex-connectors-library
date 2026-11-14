// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package repositoryflow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gitcode/go"
	"github.com/superdurable/dex-connectors-library/connectors/gitcode/go/gitcodemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestRepositoryFlowIdentityAndMapping(t *testing.T) {
	input := gitcode.GetRepositoryInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(gitcode.Connection{})))
	require.Equal(t, "StartGetRepository", dex.GetFinalStepType[gitcode.GetRepositoryInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := gitcodemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
