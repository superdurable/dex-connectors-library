// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package listprojects

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/huaweicloud/go"
	"github.com/superdurable/dex-connectors-library/connectors/huaweicloud/go/huaweicloudmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestListProjectsFlowIdentityAndMapping(t *testing.T) {
	input := huaweicloud.ListAccessibleProjectsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(huaweicloud.Connection{})))
	require.Equal(t, "StartListAccessibleProjects", dex.GetFinalStepType[huaweicloud.ListAccessibleProjectsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := huaweicloudmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
