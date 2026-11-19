// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package supabase

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/rest/v1"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "supabase", Name: "supabase"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("test-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	dispatchContext := singledispatchtest.NewContext("supabase-conformance", "create-row")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			dispatchContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"selectRows": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.SelectRows(), reference),
		"upsertRows": newConformanceMutationInvocation(t, scenario.Input, testContext{}, client.UpsertRows(), reference, scenario.CallID),
		"getRow":     newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.GetRow(), reference),
		"updateRow":  newConformanceMutationInvocation(t, scenario.Input, testContext{}, client.UpdateRow(), reference, scenario.CallID),
		"deleteRow":  newConformanceMutationInvocation(t, scenario.Input, testContext{}, client.DeleteRow(), reference, scenario.CallID),
		"createRow":  newConformanceMutationInvocation(t, scenario.Input, dispatchContext, client.CreateRow(), reference, scenario.CallID),
	}}
}
