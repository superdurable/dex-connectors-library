// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package organizationinspection

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/eventbrite/go"
	"github.com/superdurable/dex-connectors-library/connectors/eventbrite/go/eventbritemock"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestOrganizationInspectionFlowIdentityAndMapping(t *testing.T) {
	input := eventbrite.ListOrganizationsInput{}
	require.Equal(t, input, MapToOperationInput(input))
	require.Equal(t, FlowType, dex.GetFinalFlowType(NewFlow(eventbrite.Connection{})))
	require.Equal(t, "StartListOrganizations", dex.GetFinalStepType[eventbrite.ListOrganizationsInput](startRequest{}))
	wait, err := startRequest{}.WaitFor(nil, input)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	mock := eventbritemock.New(t, ConnectionName)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(mock.Connection())})
	require.NoError(t, err)
}
