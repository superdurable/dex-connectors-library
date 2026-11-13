// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spaceslist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/huoban/go"
	"github.com/superdurable/dex-connectors-library/connectors/huoban/go/huobanmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListSpacesFlowIdentityAndMapping(t *testing.T) {
	input := huoban.ListSpacesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(huoban.Connection{})))
	require.Equal(t, "StartListSpaces", dex.GetFinalStepType[huoban.ListSpacesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := huobanmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
