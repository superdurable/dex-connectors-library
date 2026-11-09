// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package keydiscovery

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/upstash/redis/go"
	"github.com/superdurable/dex-connectors-library/connectors/upstash/redis/go/redismock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestKeyDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := redis.GetKeyInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(redis.Connection{})))
	require.Equal(t, "StartGetKey", dex.GetFinalStepType[redis.GetKeyInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := redismock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
