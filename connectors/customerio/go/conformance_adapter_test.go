// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerio

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
	reference := sdkgo.ConnectionRef{Provider: "customerio", Name: "customerio"}
	privateKey := "sample-customerio-private-key"
	if scenario.Name == "listCampaigns-defect" {
		privateKey = ""
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {PrivateKey: sdkgo.NewSecretString(privateKey)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getCustomersByEmail": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCustomersByEmail(), reference),
		"getCampaign":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCampaign(), reference),
		"listCampaigns":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCampaigns(), reference),
		"getCampaignMetrics":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetCampaignMetrics(), reference),
	}}
}
