// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package filedownload

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/box/go"
	"github.com/superdurable/dex-connectors-library/connectors/box/go/boxmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFileDownloadFlowIdentityAndMapping(t *testing.T) {
	input := box.DownloadFileInput{FileID: "12345"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(box.Connection{})))
	require.Equal(t, "StartDownloadFile", dex.GetFinalStepType[box.DownloadFileInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := boxmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
