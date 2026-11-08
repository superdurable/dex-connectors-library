// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendmessage

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/telegram/go"
	"github.com/superdurable/dex-connectors-library/connectors/telegram/go/telegrammock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMessageSendIdentityAndMapping(t *testing.T) {
	input := telegram.SendMessageInput{ChatID: "@meridian_team", Text: "Hello from Dex"}
	require.Equal(t, input, mapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(telegram.Connection{})))
	require.Equal(t, "StartSendMessage", dex.GetFinalStepType[telegram.SendMessageInput](startRequest{}))
	mock := telegrammock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
