// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package segmenttext

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gugudata/go"
	"github.com/superdurable/dex-connectors-library/connectors/gugudata/go/gugudatamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestSegmentTextFlowIdentityAndMapping(t *testing.T) {
	input := gugudata.SegmentTextInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(gugudata.Connection{})))
	require.Equal(t, "StartSegmentText", dex.GetFinalStepType[gugudata.SegmentTextInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := gugudatamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
