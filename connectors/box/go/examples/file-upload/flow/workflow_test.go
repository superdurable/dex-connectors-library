// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fileupload

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/box/go"
	"github.com/superdurable/dex-connectors-library/connectors/box/go/boxmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestFileUploadFlowIdentityAndMapping(t *testing.T) {
	input := box.UploadFileInput{FileID: "12345", ContentBase64: "SGVsbG8="}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(box.Connection{})))
	require.Equal(t, "StartUploadFile", dex.GetFinalStepType[box.UploadFileInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := boxmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
