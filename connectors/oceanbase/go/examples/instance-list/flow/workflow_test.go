// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package instancelist

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/oceanbase/go"
	"github.com/superdurable/dex-connectors-library/connectors/oceanbase/go/oceanbasemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestInstanceListFlowIdentityAndMapping(t *testing.T) {
	input := oceanbase.ListInstancesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(oceanbase.Connection{})))
	require.Equal(t, "StartListInstances", dex.GetFinalStepType[oceanbase.ListInstancesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := oceanbasemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
