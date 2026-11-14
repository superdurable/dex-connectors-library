// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package currentweather

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/caiyun/go"
	"github.com/superdurable/dex-connectors-library/connectors/caiyun/go/caiyunmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestCurrentWeatherFlowIdentityAndMapping(t *testing.T) {
	input := caiyun.GetCurrentWeatherInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(caiyun.Connection{})))
	require.Equal(t, "StartGetCurrentWeather", dex.GetFinalStepType[caiyun.GetCurrentWeatherInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := caiyunmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
