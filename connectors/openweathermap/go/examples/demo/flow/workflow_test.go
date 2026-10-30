// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package demo

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/openweathermap/go"
	"github.com/superdurable/dex-connectors-library/connectors/openweathermap/go/openweathermapmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestDemoFlowIdentityAndMapping(t *testing.T) {
	input := openweathermap.WeatherInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(openweathermap.Connection{})))
	require.Equal(t, "StartGetCurrentWeather", dex.GetFinalStepType[openweathermap.WeatherInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := openweathermapmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
