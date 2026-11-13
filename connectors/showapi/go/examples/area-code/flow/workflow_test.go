// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package areacode

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/showapi/go"
	"github.com/superdurable/dex-connectors-library/connectors/showapi/go/showapimock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestAreaCodesFlowIdentityAndMapping(t *testing.T) {
	input := showapi.LookupAreaCodesInput{Area: "丽江"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(showapi.Connection{})))
	require.Equal(t, "StartLookupAreaCodes", dex.GetFinalStepType[showapi.LookupAreaCodesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := showapimock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
