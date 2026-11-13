// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package policycatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/enlvon/go"
	"github.com/superdurable/dex-connectors-library/connectors/enlvon/go/enlvonmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPolicyCatalogFlowIdentityAndMapping(t *testing.T) {
	input := enlvon.SearchPoliciesInput{Query: "分时电价", TopK: 1}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(enlvon.Connection{})))
	require.Equal(t, "StartSearchPolicies", dex.GetFinalStepType[enlvon.SearchPoliciesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := enlvonmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
