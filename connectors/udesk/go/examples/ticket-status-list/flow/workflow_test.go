// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ticketstatuslist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/udesk/go"
	"github.com/superdurable/dex-connectors-library/connectors/udesk/go/udeskmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestTicketStatusListFlowIdentityAndMapping(t *testing.T) {
	input := udesk.ListTicketStatusesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(udesk.Connection{})))
	require.Equal(t, "StartListTicketStatuses", dex.GetFinalStepType[udesk.ListTicketStatusesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := udeskmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
