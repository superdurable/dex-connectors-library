// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flightstatus

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/variflight/go"
	"github.com/superdurable/dex-connectors-library/connectors/variflight/go/variflightmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetFlightStatusFlowIdentityAndMapping(t *testing.T) {
	input := variflight.GetFlightStatusInput{FlightNumber: "GX8915", DepartureDate: "2026-11-13"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(variflight.Connection{})))
	require.Equal(t, "StartGetFlightStatus", dex.GetFinalStepType[variflight.GetFlightStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := variflightmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
