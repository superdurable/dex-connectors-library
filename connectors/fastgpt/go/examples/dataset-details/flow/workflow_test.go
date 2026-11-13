// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datasetdetails

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/fastgpt/go"
	"github.com/superdurable/dex-connectors-library/connectors/fastgpt/go/fastgptmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDatasetDetailsFlowIdentityAndMapping(t *testing.T) {
	input := fastgpt.GetDatasetDetailsInput{ID: "65abc9bd9d1448617cba5e6c"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(fastgpt.Connection{})))
	require.Equal(t, "StartGetDatasetDetails", dex.GetFinalStepType[fastgpt.GetDatasetDetailsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := fastgptmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
