// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoneread

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/qingcloud/go"
	"github.com/superdurable/dex-connectors-library/connectors/qingcloud/go/qingcloudmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestZoneReadFlowIdentityAndMapping(t *testing.T) {
	input := qingcloud.DescribeZonesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(qingcloud.Connection{})))
	require.Equal(t, "StartDescribeZones", dex.GetFinalStepType[qingcloud.DescribeZonesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := qingcloudmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
