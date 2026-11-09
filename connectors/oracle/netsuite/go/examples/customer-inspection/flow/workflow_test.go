// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/oracle/netsuite/go"
	"github.com/superdurable/dex-connectors-library/connectors/oracle/netsuite/go/oraclenetsuitemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCustomerInspectionFlowIdentityAndMapping(t *testing.T) {
	input := oraclenetsuite.GetCustomerInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(oraclenetsuite.Connection{})))
	require.Equal(t, "StartGetCustomer", dex.GetFinalStepType[oraclenetsuite.GetCustomerInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := oraclenetsuitemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
