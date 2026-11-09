// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package featurediscovery

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/productboard/go"
	"github.com/superdurable/dex-connectors-library/connectors/productboard/go/productboardmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFeatureDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := productboard.ListFeaturesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(productboard.Connection{})))
	require.Equal(t, "StartListFeatures", dex.GetFinalStepType[productboard.ListFeaturesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := productboardmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
