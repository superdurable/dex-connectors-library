// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package istero

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
	credentials := Credentials{APIKey: sdkgo.NewSecretString(testAPIKey), ISID: "60066023"}
	if scenario.Name == "getAccountStatus-defect" {
		credentials = Credentials{}
	}
	if scenario.Name == "getAccountStatus-invalidISID" {
		credentials.ISID = "060066023"
	}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getAccountStatus": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAccountStatus(), testConnection),
	}}
}
