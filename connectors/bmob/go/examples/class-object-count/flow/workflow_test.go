// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package classobjectcount

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bmob/go"
	"github.com/superdurable/dex-connectors-library/connectors/bmob/go/bmobmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestClassObjectCountFlowIdentityAndMapping(t *testing.T) {
	input := bmob.CountClassObjectsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(bmob.Connection{})))
	require.Equal(t, "StartCountClassObjects", dex.GetFinalStepType[bmob.CountClassObjectsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := bmobmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
