// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package companycatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/lixinger/go"
	"github.com/superdurable/dex-connectors-library/connectors/lixinger/go/lixingermock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCompanyCatalogFlowIdentityAndMapping(t *testing.T) {
	input := lixinger.ListStockCompaniesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(lixinger.Connection{})))
	require.Equal(t, "StartListStockCompanies", dex.GetFinalStepType[lixinger.ListStockCompaniesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := lixingermock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
