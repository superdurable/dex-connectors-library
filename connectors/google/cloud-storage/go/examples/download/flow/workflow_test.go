// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package download

import (
	"testing"

	"github.com/stretchr/testify/require"
	cloudstorage "github.com/superdurable/dex-connectors-library/connectors/google/cloud-storage/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/cloud-storage/go/cloudstoragemock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDownloadFlowIdentityAndMapping(t *testing.T) {
	input := cloudstorage.DownloadObjectInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(cloudstorage.Connection{})))
	require.Equal(t, "StartDownloadObject", dex.GetFinalStepType[cloudstorage.DownloadObjectInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := cloudstoragemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
