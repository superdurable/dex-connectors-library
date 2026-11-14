// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package holidaylookup

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tianapi/go"
	"github.com/superdurable/dex-connectors-library/connectors/tianapi/go/tianapimock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetHolidayStatusFlowIdentityAndMapping(t *testing.T) {
	input := tianapi.GetHolidayStatusInput{Date: "2021-01-01"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(tianapi.Connection{})))
	require.Equal(t, "StartGetHolidayStatus", dex.GetFinalStepType[tianapi.GetHolidayStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := tianapimock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
