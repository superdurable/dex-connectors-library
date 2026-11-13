// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package modelcatalog

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/opencsg/go"
	"github.com/superdurable/dex-connectors-library/connectors/opencsg/go/opencsgmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListGatewayModelsFlowIdentityAndMapping(t *testing.T) {
	input := opencsg.ListGatewayModelsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(opencsg.Connection{})))
	require.Equal(t, "StartListGatewayModels", dex.GetFinalStepType[opencsg.ListGatewayModelsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := opencsgmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
