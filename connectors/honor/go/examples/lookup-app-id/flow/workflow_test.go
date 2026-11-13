// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package appidlookup

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/honor/go"
	"github.com/superdurable/dex-connectors-library/connectors/honor/go/honormock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestLookupAppIDFlowIdentityAndMapping(t *testing.T) {
	input := honor.LookupAppIDInput{PackageName: "com.example.app"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(honor.Connection{})))
	require.Equal(t, "StartLookupAppID", dex.GetFinalStepType[honor.LookupAppIDInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := honormock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
