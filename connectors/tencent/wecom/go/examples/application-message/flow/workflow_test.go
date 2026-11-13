// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package applicationmessage

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tencent/wecom/go"
	"github.com/superdurable/dex-connectors-library/connectors/tencent/wecom/go/wecommock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestApplicationMessageIdentityAndMapping(t *testing.T) {
	input := wecom.SendApplicationMessageInput{ToUserIDs: []string{"zhangsan"}, MessageType: wecom.ApplicationMessageText, Content: "Incident INC-7 is open"}
	require.Equal(t, input, mapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(wecom.Connection{})))
	require.Equal(t, "StartApplicationMessage", dex.GetFinalStepType[wecom.SendApplicationMessageInput](startRequest{}))
	require.Equal(t, "wecom-app", ConnectionName)
	mock := wecommock.New(t, ConnectionName)
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
