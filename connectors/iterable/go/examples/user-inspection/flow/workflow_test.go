// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package userinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/iterable/go"
	"github.com/superdurable/dex-connectors-library/connectors/iterable/go/iterablemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestUserInspectionFlowIdentityAndMapping(t *testing.T) {
	input := iterable.GetUserInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(iterable.Connection{})))
	require.Equal(t, "StartGetUser", dex.GetFinalStepType[iterable.GetUserInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := iterablemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
