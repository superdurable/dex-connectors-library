// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package square

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
	connection := sdkgo.ConnectionRef{Provider: "square", Name: "square"}
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("test-token")}
	if scenario.Name == "listPayments-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{connection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listPayments": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPayments(), connection),
		"getPayment":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPayment(), connection),
	}}
}
