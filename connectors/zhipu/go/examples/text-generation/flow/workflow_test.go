// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textgeneration

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zhipu/go"
	"github.com/superdurable/dex-connectors-library/connectors/zhipu/go/zhipumock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestTextGenerationFlowIdentityAndMapping(t *testing.T) {
	input := zhipu.GenerateTextInput{Prompt: "Summarize the connector release."}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(zhipu.Connection{})))
	require.Equal(t, "StartGenerateText", dex.GetFinalStepType[zhipu.GenerateTextInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := zhipumock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
