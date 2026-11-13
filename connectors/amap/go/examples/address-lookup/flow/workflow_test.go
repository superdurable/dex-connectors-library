// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package addresslookup

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amap/go"
	"github.com/superdurable/dex-connectors-library/connectors/amap/go/amapmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestAddressLookupFlowIdentityAndMapping(t *testing.T) {
	input := amap.GeocodeAddressInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(amap.Connection{})))
	require.Equal(t, "StartGeocodeAddress", dex.GetFinalStepType[amap.GeocodeAddressInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := amapmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
