// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/paypal/go"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestRequestMappingAndFlowIdentity(t *testing.T) {
	request := Request{OrderID: "ORDER101"}
	prepared, err := PrepareRequest(request)
	require.NoError(t, err)
	require.Equal(t, paypal.GetOrderInput{OrderID: "ORDER101"}, MapToOperationInput(prepared))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(paypal.Connection{})))
	require.Equal(t, "PrepareResourceRead", dex.GetFinalStepType[Request](prepareRequest{}))
}
