// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package statuscounts

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/fiftyonetracking/go"
	"github.com/superdurable/dex-connectors-library/connectors/fiftyonetracking/go/fiftyonetrackingmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestShipmentStatusCountsFlowIdentityAndMapping(t *testing.T) {
	input := fiftyonetracking.GetShipmentStatusCountsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(fiftyonetracking.Connection{})))
	require.Equal(t, "StartGetShipmentStatusCounts", dex.GetFinalStepType[fiftyonetracking.GetShipmentStatusCountsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := fiftyonetrackingmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
