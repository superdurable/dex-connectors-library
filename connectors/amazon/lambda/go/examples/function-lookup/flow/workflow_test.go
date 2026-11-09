// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package functionlookup

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/lambda/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/lambda/go/lambdamock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFunctionLookupFlowIdentityAndMapping(t *testing.T) {
	input := lambda.GetFunctionInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(lambda.Connection{})))
	require.Equal(t, "StartGetFunction", dex.GetFinalStepType[lambda.GetFunctionInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := lambdamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
