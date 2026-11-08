// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customers

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/ads/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/ads/go/adsmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCustomersFlowIdentityAndMapping(t *testing.T) {
	input := ads.ListAccessibleCustomersInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(ads.Connection{})))
	require.Equal(t, "StartListAccessibleCustomers", dex.GetFinalStepType[ads.ListAccessibleCustomersInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := adsmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
