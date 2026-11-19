// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package minimax

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString(testAPIKey)}
	if scenario.Name == "getTokenPlanRemains-defect" {
		credentials = Credentials{APIKey: sdkgo.NewSecretString(strings.Repeat("x", 513))}
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getTokenPlanRemains": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTokenPlanRemains(), testConnection),
	}}
}
