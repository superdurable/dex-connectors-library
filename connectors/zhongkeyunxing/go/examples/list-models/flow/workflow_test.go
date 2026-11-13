// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package modelcatalog

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zhongkeyunxing/go"
	"github.com/superdurable/dex-connectors-library/connectors/zhongkeyunxing/go/zhongkeyunxingmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListModelsFlowIdentityAndMapping(t *testing.T) {
	input := zhongkeyunxing.ListModelsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(zhongkeyunxing.Connection{})))
	require.Equal(t, "StartListModels", dex.GetFinalStepType[zhongkeyunxing.ListModelsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := zhongkeyunxingmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
