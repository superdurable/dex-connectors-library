// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentdiscovery

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/lucid/go"
	"github.com/superdurable/dex-connectors-library/connectors/lucid/go/lucidmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDocumentDiscoveryFlowIdentityAndMapping(t *testing.T) {
	input := lucid.ListDocumentsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(lucid.Connection{})))
	require.Equal(t, "StartListDocuments", dex.GetFinalStepType[lucid.ListDocumentsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := lucidmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
