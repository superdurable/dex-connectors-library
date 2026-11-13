// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sitetdk

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/chinaz/go"
	"github.com/superdurable/dex-connectors-library/connectors/chinaz/go/chinazmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestSiteTDKFlowIdentityAndMapping(t *testing.T) {
	input := chinaz.GetSiteTDKInput{Domain: "example.com"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(chinaz.Connection{})))
	require.Equal(t, "StartGetSiteTDK", dex.GetFinalStepType[chinaz.GetSiteTDKInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := chinazmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
