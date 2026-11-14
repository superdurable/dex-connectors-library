// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package currentweather

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/qweather/go"
	"github.com/superdurable/dex-connectors-library/connectors/qweather/go/qweathermock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetCurrentWeatherFlowIdentityAndMapping(t *testing.T) {
	input := qweather.GetCurrentWeatherInput{Latitude: 39.92, Longitude: 116.41}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(qweather.Connection{})))
	require.Equal(t, "StartGetCurrentWeather", dex.GetFinalStepType[qweather.GetCurrentWeatherInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := qweathermock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
