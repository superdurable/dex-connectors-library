// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bitbucket"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestRequestMappingAndFlowIdentity(t *testing.T) {
	request := Request{Workspace: "meridian"}
	prepared, err := PrepareRequest(request)
	require.NoError(t, err)
	require.Equal(t, bitbucket.ListRepositoriesInput{Workspace: "meridian"}, MapToOperationInput(prepared))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(bitbucket.Connection{})))
	require.Equal(t, "PrepareResourceRead", dex.GetFinalStepType[Request](prepareRequest{}))
}
