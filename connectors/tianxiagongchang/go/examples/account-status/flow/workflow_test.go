// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountstatus

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tianxiagongchang/go"
	"github.com/superdurable/dex-connectors-library/connectors/tianxiagongchang/go/tianxiagongchangmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestAccountStatusFlowIdentityAndMapping(t *testing.T) {
	input := tianxiagongchang.GetAccountStatusInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(tianxiagongchang.Connection{})))
	require.Equal(t, "StartGetAccountStatus", dex.GetFinalStepType[tianxiagongchang.GetAccountStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := tianxiagongchangmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
