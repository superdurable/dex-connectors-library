// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package iplookup

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/threatbook/go"
	"github.com/superdurable/dex-connectors-library/connectors/threatbook/go/threatbookmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestCheckIPReputationFlowIdentityAndMapping(t *testing.T) {
	input := threatbook.CheckIPReputationInput{IPAddress: "118.28.8.8"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(threatbook.Connection{})))
	require.Equal(t, "StartCheckIPReputation", dex.GetFinalStepType[threatbook.CheckIPReputationInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := threatbookmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
