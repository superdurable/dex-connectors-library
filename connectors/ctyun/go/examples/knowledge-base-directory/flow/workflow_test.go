// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package knowledgebasecatalog

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/ctyun/go"
	"github.com/superdurable/dex-connectors-library/connectors/ctyun/go/ctyunmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestKnowledgeBaseCatalogFlowIdentityAndMapping(t *testing.T) {
	input := ctyun.ListKnowledgeBasesInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(ctyun.Connection{})))
	require.Equal(t, "StartListKnowledgeBases", dex.GetFinalStepType[ctyun.ListKnowledgeBasesInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := ctyunmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
