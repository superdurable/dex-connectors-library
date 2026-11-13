// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package translationflow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/niutrans/go"
	"github.com/superdurable/dex-connectors-library/connectors/niutrans/go/niutransmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestTranslationFlowIdentityAndMapping(t *testing.T) {
	input := niutrans.TranslateTextInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(niutrans.Connection{})))
	require.Equal(t, "StartTranslateText", dex.GetFinalStepType[niutrans.TranslateTextInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := niutransmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
