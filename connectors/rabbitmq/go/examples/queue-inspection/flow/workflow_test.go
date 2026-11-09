// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package queueinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/rabbitmq/go"
	"github.com/superdurable/dex-connectors-library/connectors/rabbitmq/go/rabbitmqmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestQueueInspectionFlowIdentityAndMapping(t *testing.T) {
	input := rabbitmq.GetQueueMetricsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(rabbitmq.Connection{})))
	require.Equal(t, "StartGetQueueMetrics", dex.GetFinalStepType[rabbitmq.GetQueueMetricsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := rabbitmqmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
