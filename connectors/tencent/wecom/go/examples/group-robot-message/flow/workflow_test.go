// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package grouprobotmessage

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tencent/wecom/go"
	"github.com/superdurable/dex-connectors-library/connectors/tencent/wecom/go/wecommock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestGroupRobotMessageIdentityAndMapping(t *testing.T) {
	input := wecom.SendGroupRobotMessageInput{MessageType: wecom.GroupRobotMessageMarkdown, Content: "**Deploy 42** finished"}
	require.Equal(t, input, mapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(wecom.Connection{})))
	require.Equal(t, "StartGroupRobotMessage", dex.GetFinalStepType[wecom.SendGroupRobotMessageInput](startRequest{}))
	require.Equal(t, "wecom-robot", ConnectionName)
	mock := wecommock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
