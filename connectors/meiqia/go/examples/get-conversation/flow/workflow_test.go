// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package conversationread

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/meiqia/go"
	"github.com/superdurable/dex-connectors-library/connectors/meiqia/go/meiqiamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetConversationFlowIdentityAndMapping(t *testing.T) {
	input := meiqia.GetConversationInput{ConversationID: 123}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(meiqia.Connection{})))
	require.Equal(t, "StartGetConversation", dex.GetFinalStepType[meiqia.GetConversationInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := meiqiamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
