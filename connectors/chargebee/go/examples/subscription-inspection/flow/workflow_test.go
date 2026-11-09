// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package subscriptioninspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/chargebee/go"
	"github.com/superdurable/dex-connectors-library/connectors/chargebee/go/chargebeemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSubscriptionInspectionFlowIdentityAndMapping(t *testing.T) {
	input := chargebee.GetSubscriptionInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(chargebee.Connection{})))
	require.Equal(t, "StartGetSubscription", dex.GetFinalStepType[chargebee.GetSubscriptionInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := chargebeemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
