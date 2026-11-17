// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elasticsearch

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := New(Config{ClusterURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {Username: "dex_reader", Password: sdkgo.NewSecretString("fake-password")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("elasticsearch-provider-test", "query")
	priorAttempt := 0
	if len(scenario.PriorHeartbeat) > 0 {
		var marker struct {
			Attempt int             `json:"attempt"`
			Value   json.RawMessage `json:"value"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		checkpoint := scenario.PriorHeartbeat
		if len(marker.Value) > 0 {
			checkpoint = marker.Value
		}
		require.NoError(t, context.SetPersistedCheckpoint(checkpoint))
		priorAttempt = marker.Attempt
		for range priorAttempt {
			context.AdvanceAttempt()
		}
	}
	currentAttempt := priorAttempt + 1
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		for currentAttempt < priorAttempt+attempt {
			context.AdvanceAttempt()
			currentAttempt++
		}
	}, actions: map[string]conformanceOperationInvocation{
		"createIndex":     newConformanceMutationInvocation(t, scenario.Input, context, client.CreateIndex(), testConnection, scenario.CallID),
		"deleteIndex":     newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteIndex(), testConnection, scenario.CallID),
		"getIndex":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetIndex(), testConnection),
		"listIndices":     newConformanceQueryInvocation(t, scenario.Input, context, client.ListIndices(), testConnection),
		"getDocument":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetDocument(), testConnection),
		"indexDocument":   newConformanceMutationInvocation(t, scenario.Input, context, client.IndexDocument(), testConnection, scenario.CallID),
		"createDocument":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateDocument(), testConnection, scenario.CallID),
		"updateDocument":  newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateDocument(), testConnection, scenario.CallID),
		"deleteDocument":  newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteDocument(), testConnection, scenario.CallID),
		"searchDocuments": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchDocuments(), testConnection),
	}}
}
