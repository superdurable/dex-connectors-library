// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package knowledgecatalog

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/tiankechat/go"
	"github.com/superdurable/dex-connectors-library/connectors/tiankechat/go/tiankechatmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestKnowledgeCatalogFlowIdentityAndMapping(t *testing.T) {
	input := tiankechat.ListKnowledgeEntriesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(tiankechat.Connection{})))
	require.Equal(t, "StartListKnowledgeEntries", dex.GetFinalStepType[tiankechat.ListKnowledgeEntriesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := tiankechatmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
