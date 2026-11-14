// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package productsearch

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/smzdm/go"
	"github.com/superdurable/dex-connectors-library/connectors/smzdm/go/smzdmmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestProductsFlowIdentityAndMapping(t *testing.T) {
	input := smzdm.SearchProductsInput{Keyword: "笔记本"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(smzdm.Connection{})))
	require.Equal(t, "StartSearchProducts", dex.GetFinalStepType[smzdm.SearchProductsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := smzdmmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
