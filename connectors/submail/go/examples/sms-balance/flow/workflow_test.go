// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smsbalance

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/submail/go"
	"github.com/superdurable/dex-connectors-library/connectors/submail/go/submailmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSMSBalanceFlowIdentityAndMapping(t *testing.T) {
	input := submail.GetSMSBalanceInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(submail.Connection{})))
	require.Equal(t, "StartGetSMSBalance", dex.GetFinalStepType[submail.GetSMSBalanceInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := submailmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
