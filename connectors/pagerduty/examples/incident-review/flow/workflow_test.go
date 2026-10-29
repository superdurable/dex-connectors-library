// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package incidentreview

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pagerduty"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"testing"
)

func TestFlowIdentityAndMapping(t *testing.T) {
	require.Equal(t, "PagerDutyIncidentReview", dex.GetFinalFlowType(NewFlow(pagerduty.Connection{})))
	require.Equal(t, "PrepareRequest", dex.GetFinalStepType[Request](prepareRequest{}))
	require.Equal(t, "CompleteRequest", dex.GetFinalStepType[pagerduty.ListIncidentsResult](completeRequest{}))
	request := Request(pagerduty.ListIncidentsInput{Limit: 25})
	mapped := pagerduty.ListIncidentsInput(request)
	require.Equal(t, request, Request(mapped))
	wait, err := prepareRequest{}.WaitFor(nil, request)
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	reference := sdkgo.ConnectionRef{Provider: "pagerduty", Name: ConnectionName}
	mockConnection, err := pagerduty.NewConnectionWithOperations(nil, reference)
	require.Error(t, err)
	require.Equal(t, pagerduty.Connection{}, mockConnection)
}
