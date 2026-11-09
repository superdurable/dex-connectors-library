// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/personio/go"
	"github.com/superdurable/dex-connectors-library/connectors/personio/go/personiomock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPersonInspectionFlowIdentityAndMapping(t *testing.T) {
	input := personio.GetPersonInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(personio.Connection{})))
	require.Equal(t, "StartGetPerson", dex.GetFinalStepType[personio.GetPersonInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := personiomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
