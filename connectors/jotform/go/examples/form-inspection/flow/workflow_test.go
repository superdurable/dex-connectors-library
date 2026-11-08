// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package forminspection

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/jotform/go"
	"github.com/superdurable/dex-connectors-library/connectors/jotform/go/jotformmock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestFormInspectionFlowIdentityAndMapping(t *testing.T) {
	input := jotform.ListFormsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(jotform.Connection{})))
	require.Equal(t, "StartListForms", dex.GetFinalStepType[jotform.ListFormsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := jotformmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
