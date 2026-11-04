// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/superdurable/dex-connectors-library/connectors/notion/go"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client := newNotionClient(t, endpoint, notion.WithHTTPClient(transport))
	context := singledispatchtest.NewContext("notion-conformance", scenario.Name)
	if len(scenario.PriorHeartbeat) > 0 {
		var marker any
		if err := json.Unmarshal(scenario.PriorHeartbeat, &marker); err != nil {
			t.Fatal(err)
		}
		if err := context.SetPersistedCheckpoint(marker); err != nil {
			t.Fatal(err)
		}
		context.AdvanceAttempt()
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"search":               newConformanceQueryInvocation(t, scenario.Input, context, client.Search(), notionConnection),
		"queryDatabase":        newConformanceQueryInvocation(t, scenario.Input, context, client.QueryDatabase(), notionConnection),
		"getPage":              newConformanceQueryInvocation(t, scenario.Input, context, client.GetPage(), notionConnection),
		"createPage":           newConformanceMutationInvocation(t, scenario.Input, context, client.CreatePage(), notionConnection, scenario.CallID),
		"updatePageProperties": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdatePageProperties(), notionConnection, scenario.CallID),
	}}
}
