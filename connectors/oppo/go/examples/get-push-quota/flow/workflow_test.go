// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pushquota

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/oppo/go"
	"github.com/superdurable/dex-connectors-library/connectors/oppo/go/oppomock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetPushQuotaFlowIdentityAndMapping(t *testing.T) {
	input := oppo.GetPushQuotaInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(oppo.Connection{})))
	require.Equal(t, "StartGetPushQuota", dex.GetFinalStepType[oppo.GetPushQuotaInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := oppomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
