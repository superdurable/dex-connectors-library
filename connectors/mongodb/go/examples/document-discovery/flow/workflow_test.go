// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentdiscovery

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mongodb/go"
	"github.com/superdurable/dex-connectors-library/connectors/mongodb/go/mongodbmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDocumentDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := mongodb.FindDocumentsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(mongodb.Connection{})))
	require.Equal(t, "StartFindDocuments", dex.GetFinalStepType[mongodb.FindDocumentsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := mongodbmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
