// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package folderfiles

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/kingsoftoffice/go"
	"github.com/superdurable/dex-connectors-library/connectors/kingsoftoffice/go/kingsoftofficemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestFolderFilesFlowIdentityAndMapping(t *testing.T) {
	input := kingsoftoffice.ListFolderFilesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(kingsoftoffice.Connection{})))
	require.Equal(t, "StartListFolderFiles", dex.GetFinalStepType[kingsoftoffice.ListFolderFilesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := kingsoftofficemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
