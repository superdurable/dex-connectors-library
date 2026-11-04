// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paypal

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
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {ClientID: "client-test", ClientSecret: sdkgo.NewSecretString("secret-test")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getOrder":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOrder(), testConnection),
		"listTransactions": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTransactions(), testConnection),
	}}
}
