// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package copper

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/developer_api/v1"
const conformanceFixturePathPrefix = "/developer_api/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "copper", Name: "copper"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token"), UserEmail: sdkgo.NewSecretString("user@example.com")}}
	if scenario.Name == "listLeadStatuses-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listLeadStatuses": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListLeadStatuses(), reference),
		"getLead":          newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetLead(), reference),
	}}
}
