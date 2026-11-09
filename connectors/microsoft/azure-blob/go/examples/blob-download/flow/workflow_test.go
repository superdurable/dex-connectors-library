// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package blobdownload

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/azure-blob/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/azure-blob/go/azureblobmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestBlobDownloadFlowInputAndMapping(t *testing.T) {
	input := azureblob.GetBlobInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(azureblob.Connection{})))
	require.Equal(t, "StartGetBlob", dex.GetFinalStepType[azureblob.GetBlobInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := azureblobmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
