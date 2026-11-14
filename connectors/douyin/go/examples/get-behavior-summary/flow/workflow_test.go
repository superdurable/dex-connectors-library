// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package behaviorsummary

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/douyin/go"
	"github.com/superdurable/dex-connectors-library/connectors/douyin/go/douyinmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetBehaviorSummaryFlowIdentityAndMapping(t *testing.T) {
	input := douyin.GetBehaviorSummaryInput{Date: "2026-11-11"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(douyin.Connection{})))
	require.Equal(t, "StartGetBehaviorSummary", dex.GetFinalStepType[douyin.GetBehaviorSummaryInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := douyinmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
