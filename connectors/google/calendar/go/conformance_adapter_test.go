// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar/go"
)

const conformanceEndpointPath, conformanceFixturePathPrefix = "/calendar/v3", "/calendar/v3"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := calendar.New(calendar.Config{Endpoint: endpoint}, staticCalendarCredentials(), calendar.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newCalendarDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listEvents": newConformanceQueryInvocation(t, scenario.Input, context, client.ListEvents(), calendarConnection),
		"getEvent": newConformanceQueryInvocation(t, scenario.Input, context, client.GetEvent(), calendarConnection),
		"createEvent": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateEvent(), calendarConnection, scenario.CallID),
		"updateEvent": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateEvent(), calendarConnection, scenario.CallID),
		"queryFreeBusy": newConformanceQueryInvocation(t, scenario.Input, context, client.QueryFreeBusy(), calendarConnection),
	}}
}
