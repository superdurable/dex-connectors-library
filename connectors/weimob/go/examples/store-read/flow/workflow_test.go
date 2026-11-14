// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package storeread

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/weimob/go"
	"github.com/superdurable/dex-connectors-library/connectors/weimob/go/weimobmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStoreReadFlowIdentityAndMapping(t *testing.T) {
	input := weimob.GetStoreBasicInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(weimob.Connection{})))
	require.Equal(t, "StartGetStoreBasic", dex.GetFinalStepType[weimob.GetStoreBasicInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := weimobmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
