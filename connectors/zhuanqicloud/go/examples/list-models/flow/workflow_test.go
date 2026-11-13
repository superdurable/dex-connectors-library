// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package modelcatalog

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zhuanqicloud/go"
	"github.com/superdurable/dex-connectors-library/connectors/zhuanqicloud/go/zhuanqicloudmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListModelsFlowIdentityAndMapping(t *testing.T) {
	input := zhuanqicloud.ListModelsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(zhuanqicloud.Connection{})))
	require.Equal(t, "StartListModels", dex.GetFinalStepType[zhuanqicloud.ListModelsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := zhuanqicloudmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
