// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package enterprisebasicdetails

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/qichacha/go"
	"github.com/superdurable/dex-connectors-library/connectors/qichacha/go/qichachamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestEnterpriseBasicDetailsFlowIdentityAndMapping(t *testing.T) {
	input := qichacha.GetEnterpriseBasicDetailsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(qichacha.Connection{})))
	require.Equal(t, "StartGetEnterpriseBasicDetails", dex.GetFinalStepType[qichacha.GetEnterpriseBasicDetailsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := qichachamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
