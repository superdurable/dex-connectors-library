// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textgeneration

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/qihoo360/go"
	"github.com/superdurable/dex-connectors-library/connectors/qihoo360/go/qihoo360mock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestTextGenerationFlowIdentityAndMapping(t *testing.T) {
	input := qihoo360.GenerateTextInput{Prompt: "Summarize the connector release."}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(qihoo360.Connection{})))
	require.Equal(t, "StartGenerateText", dex.GetFinalStepType[qihoo360.GenerateTextInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := qihoo360mock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
