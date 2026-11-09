// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package topics

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/pubsub/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/pubsub/go/pubsubmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTopicsFlowIdentityAndMapping(t *testing.T) {
	input := pubsub.ListTopicsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(pubsub.Connection{})))
	require.Equal(t, "StartListTopics", dex.GetFinalStepType[pubsub.ListTopicsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := pubsubmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
