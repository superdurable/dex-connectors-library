// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactionread

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/braintree/go"
	"github.com/superdurable/dex-connectors-library/connectors/braintree/go/braintreemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTransactionReadFlowIdentityAndMapping(t *testing.T) {
	input := braintree.GetTransactionInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(braintree.Connection{})))
	require.Equal(t, "StartGetTransaction", dex.GetFinalStepType[braintree.GetTransactionInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := braintreemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
