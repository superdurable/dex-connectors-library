// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package statementresults

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/redshift/go"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/redshift/go/redshiftmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestStatementResultsFlowStatementAndMapping(t *testing.T) {
	input := redshift.GetStatementResultInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(redshift.Connection{})))
	require.Equal(t, "StartGetStatementResult", dex.GetFinalStepType[redshift.GetStatementResultInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := redshiftmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
