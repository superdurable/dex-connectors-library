// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package odoo

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
	reference := sdkgo.ConnectionRef{Provider: "odoo", Name: "odoo"}
	client, err := New(Config{BaseURL: endpoint, Database: "mycompany"}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-odoo-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"searchPartners": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchPartners(), reference),
	}}
}
