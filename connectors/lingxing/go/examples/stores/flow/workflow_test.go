// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package storelist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/lingxing/go"
	"github.com/superdurable/dex-connectors-library/connectors/lingxing/go/lingxingmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestStoreListFlowIdentityAndMapping(t *testing.T) {
	input := lingxing.ListStoresInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(lingxing.Connection{})))
	require.Equal(t, "StartListStores", dex.GetFinalStepType[lingxing.ListStoresInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := lingxingmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
