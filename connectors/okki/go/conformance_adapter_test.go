// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package okki

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
	credentials := Credentials{ClientID: "client123", ClientSecret: sdkgo.NewSecretString("secretKey"), AccessToken: sdkgo.NewSecretString("jwt123")}
	if scenario.Name == "listCustomerFields-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listCustomerFields": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCustomerFields(), testConnection),
	}}
}
