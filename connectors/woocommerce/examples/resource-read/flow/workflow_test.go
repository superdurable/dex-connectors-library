// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/woocommerce"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestRequestMappingAndFlowIdentity(t *testing.T) {
	request := Request{Page: 1}
	prepared, err := PrepareRequest(request)
	require.NoError(t, err)
	require.Equal(t, woocommerce.ListOrdersInput{Page: 1}, MapToOperationInput(prepared))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(woocommerce.Connection{})))
	require.Equal(t, "PrepareResourceRead", dex.GetFinalStepType[Request](prepareRequest{}))
}
