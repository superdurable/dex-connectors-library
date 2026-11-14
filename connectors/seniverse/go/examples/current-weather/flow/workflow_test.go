// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package currentweather

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/seniverse/go"
	"github.com/superdurable/dex-connectors-library/connectors/seniverse/go/seniversemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestCurrentWeatherFlowIdentityAndMapping(t *testing.T) {
	input := seniverse.GetCurrentWeatherInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(seniverse.Connection{})))
	require.Equal(t, "StartGetCurrentWeather", dex.GetFinalStepType[seniverse.GetCurrentWeatherInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := seniversemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
