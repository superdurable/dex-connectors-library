// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package foldercontinuation

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/dropbox/go"
	"github.com/superdurable/dex-connectors-library/connectors/dropbox/go/dropboxmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestFolderContinuationFlowIdentityAndMapping(t *testing.T) {
	input := dropbox.ContinueFolderInput{Cursor: "example-cursor"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(dropbox.Connection{})))
	require.Equal(t, "StartContinueFolder", dex.GetFinalStepType[dropbox.ContinueFolderInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := dropboxmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
