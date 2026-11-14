// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package parsestatus

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/doc2x/go"
	"github.com/superdurable/dex-connectors-library/connectors/doc2x/go/doc2xmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestGetParseStatusFlowIdentityAndMapping(t *testing.T) {
	input := doc2x.GetParseStatusInput{UID: "task-123"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(doc2x.Connection{})))
	require.Equal(t, "StartGetParseStatus", dex.GetFinalStepType[doc2x.GetParseStatusInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := doc2xmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
