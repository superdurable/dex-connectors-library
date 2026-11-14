// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerfields

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/okki/go"
	"github.com/superdurable/dex-connectors-library/connectors/okki/go/okkimock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCustomerFieldsFlowIdentityAndMapping(t *testing.T) {
	input := okki.ListCustomerFieldsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(okki.Connection{})))
	require.Equal(t, "StartListCustomerFields", dex.GetFinalStepType[okki.ListCustomerFieldsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := okkimock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
