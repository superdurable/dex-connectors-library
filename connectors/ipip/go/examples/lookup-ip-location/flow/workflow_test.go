// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package iplookup

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/ipip/go"
	"github.com/superdurable/dex-connectors-library/connectors/ipip/go/ipipmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestLookupIPLocationFlowIdentityAndMapping(t *testing.T) {
	input := ipip.LookupIPLocationInput{IPAddress: "118.28.8.8"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(ipip.Connection{})))
	require.Equal(t, "StartLookupIPLocation", dex.GetFinalStepType[ipip.LookupIPLocationInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := ipipmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
