// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/shopify"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestRequestMappingAndFlowIdentity(t *testing.T) {
	request := Request{OrderID: 101}
	prepared, err := PrepareRequest(request)
	require.NoError(t, err)
	require.Equal(t, shopify.GetOrderInput{OrderID: 101}, MapToOperationInput(prepared))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(shopify.Connection{})))
	require.Equal(t, "PrepareResourceRead", dex.GetFinalStepType[Request](prepareRequest{}))
}
