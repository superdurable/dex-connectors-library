// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pushreport

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/getui/go"
	"github.com/superdurable/dex-connectors-library/connectors/getui/go/getuimock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetDailyPushReportFlowIdentityAndMapping(t *testing.T) {
	input := getui.GetDailyPushReportInput{Date: "2021-01-01"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(getui.Connection{})))
	require.Equal(t, "StartGetDailyPushReport", dex.GetFinalStepType[getui.GetDailyPushReportInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := getuimock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
