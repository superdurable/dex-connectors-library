// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package scheduledposts

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hootsuite/go"
	"github.com/superdurable/dex-connectors-library/connectors/hootsuite/go/hootsuitemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestScheduledPostsFlowIdentityAndMapping(t *testing.T) {
	input := hootsuite.ListScheduledPostsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(hootsuite.Connection{})))
	require.Equal(t, "StartListScheduledPosts", dex.GetFinalStepType[hootsuite.ListScheduledPostsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := hootsuitemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
