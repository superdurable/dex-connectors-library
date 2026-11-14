// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package objectmetadata

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/fxiaoke/go"
	"github.com/superdurable/dex-connectors-library/connectors/fxiaoke/go/fxiaokemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListObjectMetadataFlowIdentityAndMapping(t *testing.T) {
	input := fxiaoke.ListObjectMetadataInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(fxiaoke.Connection{})))
	require.Equal(t, "StartListObjectMetadata", dex.GetFinalStepType[fxiaoke.ListObjectMetadataInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := fxiaokemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
