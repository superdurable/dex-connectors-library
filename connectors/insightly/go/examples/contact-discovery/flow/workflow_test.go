// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactdiscovery

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/insightly/go"
	"github.com/superdurable/dex-connectors-library/connectors/insightly/go/insightlymock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestContactDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := insightly.ListContactsInput{Top: 20, Skip: 0}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(insightly.Connection{})))
	require.Equal(t, "StartListContacts", dex.GetFinalStepType[insightly.ListContactsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := insightlymock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
