// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package storyinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/shortcut/go"
	"github.com/superdurable/dex-connectors-library/connectors/shortcut/go/shortcutmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStoryInspectionFlowIdentityAndMapping(t *testing.T) {
	input := shortcut.GetStoryInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(shortcut.Connection{})))
	require.Equal(t, "StartGetStory", dex.GetFinalStepType[shortcut.GetStoryInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := shortcutmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
