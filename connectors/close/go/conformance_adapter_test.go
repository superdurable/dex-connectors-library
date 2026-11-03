// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package close

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api/v1"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	connection := sdkgo.ConnectionRef{Provider: "close", Name: "close"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		connection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listLeads": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListLeads(), connection),
		"getLead":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetLead(), connection),
	}}
}
