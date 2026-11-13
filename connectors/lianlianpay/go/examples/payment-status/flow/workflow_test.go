// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paymentstatus

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/lianlianpay/go"
	"github.com/superdurable/dex-connectors-library/connectors/lianlianpay/go/lianlianpaymock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestPaymentStatusFlowIdentityAndMapping(t *testing.T) {
	input := lianlianpay.QueryPaymentStatusInput{MerchantOrderID: "ORDER123"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(lianlianpay.Connection{})))
	require.Equal(t, "StartQueryPaymentStatus", dex.GetFinalStepType[lianlianpay.QueryPaymentStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := lianlianpaymock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
