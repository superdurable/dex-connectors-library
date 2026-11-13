// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package productsearch

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/onebound/go"
	"github.com/superdurable/dex-connectors-library/connectors/onebound/go/oneboundmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestProductSearchItemListFlowIdentityAndMapping(t *testing.T) {
	input := onebound.SearchProductsInput{Keyword: "gloves"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(onebound.Connection{})))
	require.Equal(t, "StartSearchProducts", dex.GetFinalStepType[onebound.SearchProductsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := oneboundmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
