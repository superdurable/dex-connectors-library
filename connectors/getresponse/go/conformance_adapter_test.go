// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package getresponse

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = "/v3"
const conformanceFixturePathPrefix = "/v3"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "getresponse", Name: "getresponse"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	mutationContext := singledispatchtest.NewContext("getresponse-provider-test", "query")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			mutationContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"listContacts":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContacts(), reference),
		"getContact":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
		"createContact": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.CreateContact(), reference, scenario.CallID),
		"updateContact": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.UpdateContact(), reference, scenario.CallID),
		"deleteContact": newConformanceMutationInvocation(t, scenario.Input, mutationContext, client.DeleteContact(), reference, scenario.CallID),
	}}
}
