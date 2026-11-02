// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello_test

import (
	"net/http"
	"testing"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/go"
)

const conformanceEndpointPath = "/1"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client := newTrelloClient(t, endpoint, trello.WithHTTPClient(transport))
	context := newTrelloDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.previousHeartbeat = context.recordedHeartbeat
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listCards":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListCards(), trelloConnection),
		"getCard":    newConformanceQueryInvocation(t, scenario.Input, context, client.GetCard(), trelloConnection),
		"createCard": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateCard(), trelloConnection, scenario.CallID),
		"updateCard": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateCard(), trelloConnection, scenario.CallID),
		"addComment": newConformanceMutationInvocation(t, scenario.Input, context, client.AddComment(), trelloConnection, scenario.CallID),
	}}
}
