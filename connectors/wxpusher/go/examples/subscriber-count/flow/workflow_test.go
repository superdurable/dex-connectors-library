// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package subscribercount

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/wxpusher/go"
	"github.com/superdurable/dex-connectors-library/connectors/wxpusher/go/wxpushermock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestSubscriberCountFlowIdentityAndMapping(t *testing.T) {
	input := wxpusher.GetSubscriberCountInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(wxpusher.Connection{})))
	require.Equal(t, "StartGetSubscriberCount", dex.GetFinalStepType[wxpusher.GetSubscriberCountInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := wxpushermock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
