// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clustercatalog

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zilliz/go"
	"github.com/superdurable/dex-connectors-library/connectors/zilliz/go/zillizmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListClustersFlowIdentityAndMapping(t *testing.T) {
	input := zilliz.ListClustersInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(zilliz.Connection{})))
	require.Equal(t, "StartListClusters", dex.GetFinalStepType[zilliz.ListClustersInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := zillizmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
