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
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { context.previousHeartbeat = raw })
	return &conformanceMappedInvoker{t: t, scenario: scenario,
		before: func(attempt int) {
			if attempt > 1 {
				context.previousHeartbeat = append([]byte(nil), context.recordedHeartbeat...)
			}
			context.attempt = prior + int32(attempt)
		},
		actions: map[string]conformanceOperationInvocation{
			"listCalendars":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListCalendars(), calendarConnection),
			"getCalendar":    newConformanceQueryInvocation(t, scenario.Input, context, client.GetCalendar(), calendarConnection),
			"createCalendar": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateCalendar(), calendarConnection, scenario.CallID),
			"updateCalendar": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateCalendar(), calendarConnection, scenario.CallID),
			"deleteCalendar": newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteCalendar(), calendarConnection, scenario.CallID),
			"deleteEvent":    newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteEvent(), calendarConnection, scenario.CallID),
			"listEvents":     newConformanceQueryInvocation(t, scenario.Input, context, client.ListEvents(), calendarConnection),
			"getEvent":       newConformanceQueryInvocation(t, scenario.Input, context, client.GetEvent(), calendarConnection),
			"createEvent":    newConformanceMutationInvocation(t, scenario.Input, context, client.CreateEvent(), calendarConnection, scenario.CallID),
			"updateEvent":    newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateEvent(), calendarConnection, scenario.CallID),
			"queryFreeBusy":  newConformanceQueryInvocation(t, scenario.Input, context, client.QueryFreeBusy(), calendarConnection),
		}}
}
