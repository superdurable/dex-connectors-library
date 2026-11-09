// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chatlookup

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/telegram/go"
	"github.com/superdurable/dex-connectors-library/connectors/telegram/go/telegrammock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestChatLookupIdentityAndMapping(t *testing.T) {
	input := telegram.GetChatInput{ChatID: "@meridian_team"}
	require.Equal(t, input, mapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(telegram.Connection{})))
	require.Equal(t, "StartGetChat", dex.GetFinalStepType[telegram.GetChatInput](startRequest{}))
	mock := telegrammock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
