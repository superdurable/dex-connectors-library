// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactviewdiscovery

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshsales/go"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshsales/go/freshsalesmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestContactViewDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := freshsales.ListContactViewsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(freshsales.Connection{})))
	require.Equal(t, "StartListContactViews", dex.GetFinalStepType[freshsales.ListContactViewsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := freshsalesmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
