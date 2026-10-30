// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package demo

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mailgun/go"
	"github.com/superdurable/dex-connectors-library/connectors/mailgun/go/mailgunmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestDemoFlowIdentityAndMapping(t *testing.T) {
	input := mailgun.GetEventsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(mailgun.Connection{})))
	require.Equal(t, "StartGetEvents", dex.GetFinalStepType[mailgun.GetEventsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := mailgunmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
