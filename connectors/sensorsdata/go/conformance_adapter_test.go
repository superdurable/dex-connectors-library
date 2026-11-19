// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sensorsdata

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	credentials := Credentials{APIKey: sdkgo.NewSecretString(testAPIKey)}
	if scenario.Name == "listCampaigns-invalid-credentials" {
		credentials.APIKey = sdkgo.NewSecretString("bad key")
	}
	config := Config{BaseURL: "https://family.demo.sensorsdata.cn", ProjectName: "production"}
	if scenario.Name == "listCampaigns-oversize" {
		config.MaxResponseBytes = 5
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	_ = endpoint
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listCampaigns": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCampaigns(), testConnection),
	}}
}
