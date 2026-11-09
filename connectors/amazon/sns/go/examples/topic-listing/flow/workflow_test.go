// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package topiclisting

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sns/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sns/go/snsmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTopicListingFlowTopicAndMapping(t *testing.T) {
	input := sns.ListTopicsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(sns.Connection{})))
	require.Equal(t, "StartListTopics", dex.GetFinalStepType[sns.ListTopicsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := snsmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
