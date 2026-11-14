// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package formspage

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/jinshuju/go"
	"github.com/superdurable/dex-connectors-library/connectors/jinshuju/go/jinshujumock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListFormsPageFlowIdentityAndMapping(t *testing.T) {
	input := jinshuju.ListFormsPageInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(jinshuju.Connection{})))
	require.Equal(t, "StartListFormsPage", dex.GetFinalStepType[jinshuju.ListFormsPageInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := jinshujumock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
