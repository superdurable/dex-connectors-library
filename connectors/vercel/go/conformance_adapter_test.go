// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vercel

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	token := "test-accesstoken"
	if scenario.Name == "listDeployments-defect" {
		token = ""
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString(token)}}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listDeployments": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListDeployments(), testConnection),
		"getDeployment":   newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.GetDeployment(), testConnection),
	}}
}
