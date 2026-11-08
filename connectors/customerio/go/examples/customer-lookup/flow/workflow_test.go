// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerlookup

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/customerio/go"
	"github.com/superdurable/dex-connectors-library/connectors/customerio/go/customeriomock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCustomerLookupFlowIdentityAndMapping(t *testing.T) {
	input := customerio.GetCustomersByEmailInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(customerio.Connection{})))
	require.Equal(t, "StartGetCustomersByEmail", dex.GetFinalStepType[customerio.GetCustomersByEmailInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := customeriomock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
