// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := notion.New(notion.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[notion.Credentials]{
		notionConnection: {APIToken: sdkgo.NewSecretString(testAPIToken)},
	}, notion.WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("notion-conformance", "connector")
	if len(scenario.PriorHeartbeat) != 0 {
		var checkpoint any
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &checkpoint))
		require.NoError(t, context.SetPersistedCheckpoint(checkpoint))
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
