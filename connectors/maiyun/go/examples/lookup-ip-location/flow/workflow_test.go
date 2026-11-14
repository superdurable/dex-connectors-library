// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package iplookup

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/maiyun/go"
	"github.com/superdurable/dex-connectors-library/connectors/maiyun/go/maiyunmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestLookupIPLocationFlowIdentityAndMapping(t *testing.T) {
	input := maiyun.LookupIPLocationInput{IPAddress: "118.28.8.8"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(maiyun.Connection{})))
	require.Equal(t, "StartLookupIPLocation", dex.GetFinalStepType[maiyun.LookupIPLocationInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := maiyunmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
