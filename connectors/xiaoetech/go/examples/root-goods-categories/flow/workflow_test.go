// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rootgoodscategories

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xiaoetech/go"
	"github.com/superdurable/dex-connectors-library/connectors/xiaoetech/go/xiaoetechmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestRootCategoriesFlowIdentityAndMapping(t *testing.T) {
	input := xiaoetech.ListRootGoodsCategoriesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(xiaoetech.Connection{})))
	require.Equal(t, "StartListRootGoodsCategories", dex.GetFinalStepType[xiaoetech.ListRootGoodsCategoriesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := xiaoetechmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
