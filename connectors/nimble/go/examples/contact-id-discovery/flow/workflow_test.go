// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactiddiscovery

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/nimble/go"
	"github.com/superdurable/dex-connectors-library/connectors/nimble/go/nimblemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestContactIDDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := nimble.ListContactIDsInput{Page: 1, PerPage: 20}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(nimble.Connection{})))
	require.Equal(t, "StartListContactIDs", dex.GetFinalStepType[nimble.ListContactIDsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := nimblemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
