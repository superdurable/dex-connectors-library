// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package subscriberinspection

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/kit/go"
	"github.com/superdurable/dex-connectors-library/connectors/kit/go/kitmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestSubscriberInspectionFlowIdentityAndMapping(t *testing.T) {
	input := kit.ListSubscribersInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(kit.Connection{})))
	require.Equal(t, "StartListSubscribers", dex.GetFinalStepType[kit.ListSubscribersInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := kitmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
