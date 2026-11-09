// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package profileinspection

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/braze/go"
	"github.com/superdurable/dex-connectors-library/connectors/braze/go/brazemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestProfileInspectionFlowIdentityAndMapping(t *testing.T) {
	input := braze.GetUserProfileInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(braze.Connection{})))
	require.Equal(t, "StartGetUserProfile", dex.GetFinalStepType[braze.GetUserProfileInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := brazemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
