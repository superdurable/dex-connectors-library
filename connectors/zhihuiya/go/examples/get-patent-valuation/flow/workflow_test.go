// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package valuationread

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zhihuiya/go"
	"github.com/superdurable/dex-connectors-library/connectors/zhihuiya/go/zhihuiyamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetPatentValuationFlowIdentityAndMapping(t *testing.T) {
	input := zhihuiya.GetPatentValuationInput{PublicationNumber: "JP6012277B2"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(zhihuiya.Connection{})))
	require.Equal(t, "StartGetPatentValuation", dex.GetFinalStepType[zhihuiya.GetPatentValuationInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := zhihuiyamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
