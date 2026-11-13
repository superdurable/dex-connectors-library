// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package currentweather

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/newayz/go"
	"github.com/superdurable/dex-connectors-library/connectors/newayz/go/newayzmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestCurrentWeatherFlowIdentityAndMapping(t *testing.T) {
	input := newayz.GetCurrentWeatherInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(newayz.Connection{})))
	require.Equal(t, "StartGetCurrentWeather", dex.GetFinalStepType[newayz.GetCurrentWeatherInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := newayzmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
