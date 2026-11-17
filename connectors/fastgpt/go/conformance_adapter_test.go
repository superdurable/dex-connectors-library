// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fastgpt

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/fastgpt"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	config := Config{BaseURL: endpoint + conformanceFixturePathPrefix}
	if scenario.Name == "getDatasetDetails-oversize" {
		config.MaxResponseBytes = 8
	}
	credentials := Credentials{APIKey: sdkgo.NewSecretString(testAPIKey)}
	if scenario.Name == "getDatasetDetails-missing-key" {
		credentials = Credentials{}
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getDatasetDetails": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetDatasetDetails(), testConnection),
	}}
}
