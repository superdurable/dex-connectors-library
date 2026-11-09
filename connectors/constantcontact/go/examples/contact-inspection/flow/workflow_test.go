// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/constantcontact/go"
	"github.com/superdurable/dex-connectors-library/connectors/constantcontact/go/constantcontactmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestContactInspectionFlowIdentityAndMapping(t *testing.T) {
	input := constantcontact.GetContactInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(constantcontact.Connection{})))
	require.Equal(t, "StartGetContact", dex.GetFinalStepType[constantcontact.GetContactInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := constantcontactmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
