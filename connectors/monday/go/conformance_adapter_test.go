// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
	"net/http"
	"testing"

	"github.com/superdurable/dex-connectors-library/connectors/monday/go"
)

const conformanceEndpointPath = "/v2"
const conformanceFixturePathPrefix = "/v2"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client := newMondayClient(t, endpoint, monday.WithHTTPClient(transport), monday.WithClock(conformanceFixedClock(t, scenario)))
	context := newMondayDexContext("conformance")
	priorAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { context.heartbeat = raw })
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attemptNo = priorAttempt + int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"listItems":              newConformanceQueryInvocation(t, scenario.Input, context, client.ListItems(), mondayConnection),
		"getItem":                newConformanceQueryInvocation(t, scenario.Input, context, client.GetItem(), mondayConnection),
		"createItem":             newConformanceMutationInvocation(t, scenario.Input, context, client.CreateItem(), mondayConnection, scenario.CallID),
		"updateItemColumnValues": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateItemColumnValues(), mondayConnection, scenario.CallID),
		"addUpdate":              newConformanceMutationInvocation(t, scenario.Input, context, client.AddUpdate(), mondayConnection, scenario.CallID),
	}}
}
