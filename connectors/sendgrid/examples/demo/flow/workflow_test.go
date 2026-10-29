// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package demo

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/sendgrid"
	"github.com/superdurable/dex-connectors-library/connectors/sendgrid/sendgridmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestDemoFlowIdentityAndMapping(t *testing.T) {
	input := sendgrid.GetStatsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(sendgrid.Connection{})))
	require.Equal(t, "StartGetStats", dex.GetFinalStepType[sendgrid.GetStatsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := sendgridmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
