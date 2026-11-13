// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contractstatus

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/qiyuesuo/go"
	"github.com/superdurable/dex-connectors-library/connectors/qiyuesuo/go/qiyuesuomock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestContractStatusFlowIdentityAndMapping(t *testing.T) {
	input := qiyuesuo.ListContractsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(qiyuesuo.Connection{})))
	require.Equal(t, "StartListContracts", dex.GetFinalStepType[qiyuesuo.ListContractsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := qiyuesuomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
