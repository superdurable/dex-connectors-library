// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/getresponse/go"
	"github.com/superdurable/dex-connectors-library/connectors/getresponse/go/getresponsemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestContactInspectionFlowIdentityAndMapping(t *testing.T) {
	input := getresponse.ListContactsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(getresponse.Connection{})))
	require.Equal(t, "StartListContacts", dex.GetFinalStepType[getresponse.ListContactsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := getresponsemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
