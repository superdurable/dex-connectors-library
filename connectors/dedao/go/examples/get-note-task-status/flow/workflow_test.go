// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notetaskstatus

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/dedao/go"
	"github.com/superdurable/dex-connectors-library/connectors/dedao/go/dedaomock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetNoteTaskStatusFlowIdentityAndMapping(t *testing.T) {
	input := dedao.GetNoteTaskStatusInput{TaskID: "69c3995e99f5a67e"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(dedao.Connection{})))
	require.Equal(t, "StartGetNoteTaskStatus", dex.GetFinalStepType[dedao.GetNoteTaskStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := dedaomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
