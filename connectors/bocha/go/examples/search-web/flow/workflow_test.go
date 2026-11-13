// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package websearch

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bocha/go"
	"github.com/superdurable/dex-connectors-library/connectors/bocha/go/bochamock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestSearchWebFlowIdentityAndMapping(t *testing.T) {
	input := bocha.SearchWebInput{Query: "example"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(bocha.Connection{})))
	require.Equal(t, "StartSearchWeb", dex.GetFinalStepType[bocha.SearchWebInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := bochamock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
