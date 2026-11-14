// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountquota

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/knownsec/zoomeye/go"
	"github.com/superdurable/dex-connectors-library/connectors/knownsec/zoomeye/go/zoomeyemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestAccountQuotaFlowIdentityAndMapping(t *testing.T) {
	input := zoomeye.GetAccountQuotaInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(zoomeye.Connection{})))
	require.Equal(t, "StartGetAccountQuota", dex.GetFinalStepType[zoomeye.GetAccountQuotaInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := zoomeyemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
