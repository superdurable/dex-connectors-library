// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package applist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pgyer/go"
	"github.com/superdurable/dex-connectors-library/connectors/pgyer/go/pgyermock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestUploadedAppsFlowIdentityAndMapping(t *testing.T) {
	input := pgyer.ListUploadedAppsInput{Page: 1}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(pgyer.Connection{})))
	require.Equal(t, "StartListUploadedApps", dex.GetFinalStepType[pgyer.ListUploadedAppsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := pgyermock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
