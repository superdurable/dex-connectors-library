// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectcatalog

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/apifox/go"
	"github.com/superdurable/dex-connectors-library/connectors/apifox/go/apifoxmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetProjectAPISummaryFlowIdentityAndMapping(t *testing.T) {
	input := apifox.GetProjectAPISummaryInput{ProjectID: "123"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(apifox.Connection{})))
	require.Equal(t, "StartGetProjectAPISummary", dex.GetFinalStepType[apifox.GetProjectAPISummaryInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := apifoxmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
