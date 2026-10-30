// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly/go/calendlymock"
	schedulinglink "github.com/superdurable/dex-connectors-library/connectors/calendly/go/examples/scheduling-link/flow"
)

func TestSchedulingLinkFlowDeclaresItsStaticConnectionAndViews(t *testing.T) {
	mock := calendlymock.New(t, schedulinglink.ConnectionName)
	flow := schedulinglink.NewFlow(mock.Connection())
	require.Equal(t, "CalendlyCreateSchedulingLink", flow.GetFlowType())
	require.Equal(t, "calendly-scheduling", schedulinglink.ConnectionName)
	require.Len(t, flow.GetSteps(), 2)
	require.Len(t, flow.GetRPCs(), 2)
	require.Len(t, flow.GetPersistenceSchema().Attributes, 1)
}
