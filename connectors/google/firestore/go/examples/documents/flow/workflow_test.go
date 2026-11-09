// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documents

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/firestore/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/firestore/go/firestoremock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDocumentsFlowIdentityAndMapping(t *testing.T) {
	input := firestore.ListDocumentsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(firestore.Connection{})))
	require.Equal(t, "StartListDocuments", dex.GetFinalStepType[firestore.ListDocumentsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := firestoremock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
