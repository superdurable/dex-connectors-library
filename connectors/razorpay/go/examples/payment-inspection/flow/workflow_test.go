// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paymentinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/razorpay/go"
	"github.com/superdurable/dex-connectors-library/connectors/razorpay/go/razorpaymock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPaymentInspectionFlowIdentityAndMapping(t *testing.T) {
	input := razorpay.GetPaymentInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(razorpay.Connection{})))
	require.Equal(t, "StartGetPayment", dex.GetFinalStepType[razorpay.GetPaymentInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := razorpaymock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
