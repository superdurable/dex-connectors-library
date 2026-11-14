// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package worksheets

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mingdao/go"
	"github.com/superdurable/dex-connectors-library/connectors/mingdao/go/mingdaomock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListWorksheetsFlowIdentityAndMapping(t *testing.T) {
	input := mingdao.ListWorksheetsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(mingdao.Connection{})))
	require.Equal(t, "StartListWorksheets", dex.GetFinalStepType[mingdao.ListWorksheetsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := mingdaomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
