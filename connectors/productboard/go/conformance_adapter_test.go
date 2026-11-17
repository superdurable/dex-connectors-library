// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package productboard

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/v2"
const conformanceFixturePathPrefix = "/v2"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "productboard", Name: "productboard"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIToken: sdkgo.NewSecretString("sample-productboard-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("productboard-conformance", "feature-operation")
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
		"listFeatures":   newConformanceQueryInvocation(t, scenario.Input, context, client.ListFeatures(), reference),
		"getFeature":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetFeature(), reference),
		"createFeature":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateFeature(), reference, scenario.CallID),
		"renameFeature":  newConformanceMutationInvocation(t, scenario.Input, context, client.RenameFeature(), reference, scenario.CallID),
		"deleteFeature":  newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteFeature(), reference, scenario.CallID),
		"listNotes":      newConformanceQueryInvocation(t, scenario.Input, context, client.ListNotes(), reference),
		"getNote":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetNote(), reference),
		"createTextNote": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateTextNote(), reference, scenario.CallID),
		"renameNote":     newConformanceMutationInvocation(t, scenario.Input, context, client.RenameNote(), reference, scenario.CallID),
		"archiveNote":    newConformanceMutationInvocation(t, scenario.Input, context, client.ArchiveNote(), reference, scenario.CallID),
		"deleteNote":     newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteNote(), reference, scenario.CallID),
	}}
}
