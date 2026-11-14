// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sparkchat

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/iflytek/go"
	"github.com/superdurable/dex-connectors-library/connectors/iflytek/go/iflytekmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestChatCompletionFlowIdentityAndMapping(t *testing.T) {
	input := iflytek.CreateChatCompletionInput{Prompt: "Hello"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(iflytek.Connection{})))
	require.Equal(t, "StartCreateChatCompletion", dex.GetFinalStepType[iflytek.CreateChatCompletionInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := iflytekmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
