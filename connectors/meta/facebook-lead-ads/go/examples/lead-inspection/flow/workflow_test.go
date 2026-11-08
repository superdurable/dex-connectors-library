// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadinspection

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/meta/facebook-lead-ads/go"
	"github.com/superdurable/dex-connectors-library/connectors/meta/facebook-lead-ads/go/facebookleadadsmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestLeadInspectionFlowIdentityAndMapping(t *testing.T) {
	input := facebookleadads.GetLeadInput{LeadID: "987654321012345"}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(facebookleadads.Connection{})))
	require.Equal(t, "StartGetLead", dex.GetFinalStepType[facebookleadads.GetLeadInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := facebookleadadsmock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
