// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountstatus

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/istero/go"
	"github.com/superdurable/dex-connectors-library/connectors/istero/go/isteromock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestAccountStatusFlowIdentityAndMapping(t *testing.T) {
	input := istero.GetAccountStatusInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(istero.Connection{})))
	require.Equal(t, "StartGetAccountStatus", dex.GetFinalStepType[istero.GetAccountStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := isteromock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
