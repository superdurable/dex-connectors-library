// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package post

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reddit/go"
	"github.com/superdurable/dex-connectors-library/connectors/reddit/go/redditmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPostFlowIdentityAndMapping(t *testing.T) {
	input := reddit.GetPostInput{PostID: "t3_abc123"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(reddit.Connection{})))
	require.Equal(t, "StartGetPost", dex.GetFinalStepType[reddit.GetPostInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := redditmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
