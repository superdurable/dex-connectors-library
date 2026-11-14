// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package signflowstatus

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/esign/go"
	"github.com/superdurable/dex-connectors-library/connectors/esign/go/esignmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetSignFlowStatusFlowIdentityAndMapping(t *testing.T) {
	input := esign.GetSignFlowStatusInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(esign.Connection{})))
	require.Equal(t, "StartGetSignFlowStatus", dex.GetFinalStepType[esign.GetSignFlowStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := esignmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
