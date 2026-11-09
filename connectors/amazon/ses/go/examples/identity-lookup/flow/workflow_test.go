// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package identitylookup

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/ses/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/ses/go/sesmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestIdentityLookupFlowIdentityAndMapping(t *testing.T) {
	input := ses.GetEmailIdentityInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(ses.Connection{})))
	require.Equal(t, "StartGetEmailIdentity", dex.GetFinalStepType[ses.GetEmailIdentityInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := sesmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
