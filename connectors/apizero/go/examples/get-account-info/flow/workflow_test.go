// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountinfo

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/apizero/go"
	"github.com/superdurable/dex-connectors-library/connectors/apizero/go/apizeromock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetAccountInfoFlowIdentityAndMapping(t *testing.T) {
	input := apizero.GetAccountInfoInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(apizero.Connection{})))
	require.Equal(t, "StartGetAccountInfo", dex.GetFinalStepType[apizero.GetAccountInfoInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := apizeromock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
