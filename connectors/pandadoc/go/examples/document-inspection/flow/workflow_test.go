// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package documentinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pandadoc/go"
	"github.com/superdurable/dex-connectors-library/connectors/pandadoc/go/pandadocmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDocumentInspectionFlowIdentityAndMapping(t *testing.T) {
	input := pandadoc.GetDocumentInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(pandadoc.Connection{})))
	require.Equal(t, "StartGetDocument", dex.GetFinalStepType[pandadoc.GetDocumentInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := pandadocmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
