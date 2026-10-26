// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package schedulinglink

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/connectors/calendly/calendlymock"
)

func TestSchedulingLinkFlowDeclaresItsStaticConnectionAndViews(t *testing.T) {
	mock := calendlymock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	require.Equal(t, "CalendlyCreateSchedulingLink", flow.GetFlowType())
	require.Equal(t, "calendly-scheduling", ConnectionName)
	require.Len(t, flow.GetSteps(), 2)
	require.Len(t, flow.GetRPCs(), 2)
	require.Len(t, flow.GetPersistenceSchema().Attributes, 1)
	input := calendly.CreateSchedulingLinkInput{EventTypeURI: "https://api.calendly.com/event_types/TYPE0001"}
	require.Equal(t, input, useSchedulingLinkInput(input))
}
