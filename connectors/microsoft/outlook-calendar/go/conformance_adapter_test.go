// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/v1.0"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := outlookcalendar.New(outlookcalendar.Config{}, staticCalendarCredentials(),
		outlookcalendar.WithLocalProviderURL(endpoint), outlookcalendar.WithHTTPClient(transport))
	require.NoError(t, err)
	context := newCalendarDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listEvents":    newConformanceQueryInvocation(t, scenario.Input, context, client.ListEvents(), calendarConnection),
		"getEvent":      newConformanceQueryInvocation(t, scenario.Input, context, client.GetEvent(), calendarConnection),
		"createEvent":   newConformanceMutationInvocation(t, scenario.Input, context, client.CreateEvent(), calendarConnection, scenario.CallID),
		"updateEvent":   newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateEvent(), calendarConnection, scenario.CallID),
		"queryFreeBusy": newConformanceQueryInvocation(t, scenario.Input, context, client.QueryFreeBusy(), calendarConnection),
	}}
}
