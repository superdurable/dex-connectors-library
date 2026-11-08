// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datasets

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/bigquery/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/bigquery/go/bigquerymock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDatasetsFlowIdentityAndMapping(t *testing.T) {
	input := bigquery.ListDatasetsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(bigquery.Connection{})))
	require.Equal(t, "StartListDatasets", dex.GetFinalStepType[bigquery.ListDatasetsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := bigquerymock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
