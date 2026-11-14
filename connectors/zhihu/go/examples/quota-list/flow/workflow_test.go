// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quotalist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zhihu/go"
	"github.com/superdurable/dex-connectors-library/connectors/zhihu/go/zhihumock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestQuotasFlowIdentityAndMapping(t *testing.T) {
	input := zhihu.ListQuotasInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(zhihu.Connection{})))
	require.Equal(t, "StartListQuotas", dex.GetFinalStepType[zhihu.ListQuotasInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := zhihumock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
