// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package property

import (
	"testing"

	"github.com/stretchr/testify/require"
	analytics4 "github.com/superdurable/dex-connectors-library/connectors/google/analytics/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/analytics/go/analytics4mock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPropertyFlowIdentityAndMapping(t *testing.T) {
	input := analytics4.GetPropertyInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(analytics4.Connection{})))
	require.Equal(t, "StartGetProperty", dex.GetFinalStepType[analytics4.GetPropertyInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := analytics4mock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
