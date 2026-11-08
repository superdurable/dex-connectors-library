// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package insightlist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/posthog/go"
	"github.com/superdurable/dex-connectors-library/connectors/posthog/go/posthogmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestInsightListFlowIdentityAndMapping(t *testing.T) {
	input := posthog.ListInsightsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(posthog.Connection{})))
	require.Equal(t, "StartListInsights", dex.GetFinalStepType[posthog.ListInsightsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := posthogmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
