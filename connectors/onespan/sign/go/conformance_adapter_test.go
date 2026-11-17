// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onespansign

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = "/api"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "onespan-sign", Name: "onespan-sign"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-onespan-sign-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	conformanceContext := queryContext{priorHeartbeat: scenario.PriorHeartbeat}
	if len(scenario.PriorHeartbeat) > 0 {
		var marker struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		conformanceContext.attempt = marker.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getPackage":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPackage(), reference),
		"listPackages":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPackages(), reference),
		"createDraftPackage": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateDraftPackage(), reference, scenario.CallID),
		"updatePackageName":  newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdatePackageName(), reference, scenario.CallID),
		"sendPackage":        newConformanceMutationInvocation(t, scenario.Input, conformanceContext, client.SendPackage(), reference, scenario.CallID),
		"trashPackage":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.TrashPackage(), reference, scenario.CallID),
		"deletePackage":      newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeletePackage(), reference, scenario.CallID),
		"listSignerRoles":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSignerRoles(), reference),
		"getSignerRole":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSignerRole(), reference),
		"addSigner":          newConformanceMutationInvocation(t, scenario.Input, conformanceContext, client.AddSigner(), reference, scenario.CallID),
		"updateSigner":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateSigner(), reference, scenario.CallID),
		"removeSigner":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RemoveSigner(), reference, scenario.CallID),
	}}
}
