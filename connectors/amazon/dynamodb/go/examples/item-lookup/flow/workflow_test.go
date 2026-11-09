// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package itemlookup

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/dynamodb/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/dynamodb/go/dynamodbmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestItemLookupFlowItemAndMapping(t *testing.T) {
	input := dynamodb.GetItemInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(dynamodb.Connection{})))
	require.Equal(t, "StartGetItem", dex.GetFinalStepType[dynamodb.GetItemInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := dynamodbmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
