// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package employee

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gusto/go"
	"github.com/superdurable/dex-connectors-library/connectors/gusto/go/gustomock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestEmployeeFlowIdentityAndMapping(t *testing.T) {
	input := gusto.GetEmployeeInput{EmployeeID: "90a68db5-cb3a-4f64-bb60-c0a934ee7600"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(gusto.Connection{})))
	require.Equal(t, "StartGetEmployee", dex.GetFinalStepType[gusto.GetEmployeeInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := gustomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
