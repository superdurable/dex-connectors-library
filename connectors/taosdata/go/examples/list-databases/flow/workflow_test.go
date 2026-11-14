// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package listdatabases

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/taosdata/go"
	"github.com/superdurable/dex-connectors-library/connectors/taosdata/go/taosdatamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListDatabasesFlowIdentityAndMapping(t *testing.T) {
	input := taosdata.ListDatabasesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(taosdata.Connection{})))
	require.Equal(t, "StartListDatabases", dex.GetFinalStepType[taosdata.ListDatabasesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := taosdatamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
