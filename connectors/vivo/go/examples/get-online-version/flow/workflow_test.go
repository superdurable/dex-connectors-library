// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onlineversion

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/vivo/go"
	"github.com/superdurable/dex-connectors-library/connectors/vivo/go/vivomock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetOnlineVersionFlowIdentityAndMapping(t *testing.T) {
	input := vivo.GetOnlineVersionInput{PackageName: "com.example.app"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(vivo.Connection{})))
	require.Equal(t, "StartGetOnlineVersion", dex.GetFinalStepType[vivo.GetOnlineVersionInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := vivomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
