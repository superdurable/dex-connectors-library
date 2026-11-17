// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dynamics365sales

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "microsoft-dynamics-365-sales", Name: "microsoft-dynamics-365-sales"}
	credentials := Credentials{ClientSecret: sdkgo.NewSecretString("test-client-secret")}
	client, err := New(Config{EnvironmentURL: endpoint, TenantID: testTenantID, ClientID: testClientID}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport), WithTokenEndpoint(endpoint+"/token"))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"findContact":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.FindContact(), reference),
		"listAccounts":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListAccounts(), reference),
		"getAccount":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAccount(), reference),
		"createAccount": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateAccount(), reference, scenario.CallID),
		"updateAccount": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateAccount(), reference, scenario.CallID),
		"deleteAccount": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteAccount(), reference, scenario.CallID),
	}}
}
