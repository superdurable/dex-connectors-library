// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recurly

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = "/api"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "recurly", Name: "recurly"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-recurly-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	mutationContext := singledispatchtest.NewContext("recurly-conformance", "create-account")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			mutationContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getAccount":             newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAccount(), reference),
		"listAccounts":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAccounts(), reference),
		"createAccount":          newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CreateAccount(), reference, scenario.CallID),
		"listSubscriptions":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSubscriptions(), reference),
		"getSubscription":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSubscription(), reference),
		"cancelSubscription":     newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CancelSubscription(), reference, scenario.CallID),
		"reactivateSubscription": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.ReactivateSubscription(), reference, scenario.CallID),
		"listPlans":              newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPlans(), reference),
		"getPlan":                newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPlan(), reference),
		"createPlan":             newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CreatePlan(), reference, scenario.CallID),
		"renamePlan":             newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.RenamePlan(), reference, scenario.CallID),
		"deletePlan":             newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.DeletePlan(), reference, scenario.CallID),
	}}
}
