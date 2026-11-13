// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package routequery

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/sfexpress/go"
	"github.com/superdurable/dex-connectors-library/connectors/sfexpress/go/sfexpressmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestRouteQueryFlowIdentityAndMapping(t *testing.T) {
	input := sfexpress.QueryRoutesInput{WaybillNumber: "SF1234567890123"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(sfexpress.Connection{})))
	require.Equal(t, "StartQueryRoutes", dex.GetFinalStepType[sfexpress.QueryRoutesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := sfexpressmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
