// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shopread

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/youzan/go"
	"github.com/superdurable/dex-connectors-library/connectors/youzan/go/youzanmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestShopReadFlowIdentityAndMapping(t *testing.T) {
	input := youzan.GetShopBasicInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(youzan.Connection{})))
	require.Equal(t, "StartGetShopBasic", dex.GetFinalStepType[youzan.GetShopBasicInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := youzanmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
