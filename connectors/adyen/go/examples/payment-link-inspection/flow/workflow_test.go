// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paymentlinkinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/adyen/go"
	"github.com/superdurable/dex-connectors-library/connectors/adyen/go/adyenmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPaymentLinkInspectionFlowIdentityAndMapping(t *testing.T) {
	input := adyen.GetPaymentLinkInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(adyen.Connection{})))
	require.Equal(t, "StartGetPaymentLink", dex.GetFinalStepType[adyen.GetPaymentLinkInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := adyenmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
