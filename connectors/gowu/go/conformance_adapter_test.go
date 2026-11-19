// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gowu

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
	credentials := Credentials{AppKey: "REDACTED_GOWU_APP_KEY", AppSecret: sdkgo.NewSecretString("REDACTED_GOWU_SECRET")}
	if scenario.Name == "getAccessClient-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	client.now = conformanceFixedClock(t, scenario)
	client.nonceReader = newConformanceSyntheticRandomReader(scenario.RandomSeed)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getAccessClient": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAccessClient(), testConnection),
	}}
}
