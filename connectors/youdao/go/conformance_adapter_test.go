// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youdao

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
	credentials := Credentials{AppKey: "synthetic-app-key", AppSecret: sdkgo.NewSecretString("synthetic-app-secret")}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "translateText-oversize" {
		config.MaxResponseBytes = 4
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials},
		WithHTTPClient(transport), randomSourceOption{source: newConformanceSyntheticRandomReader(scenario.RandomSeed)},
		clockOption{now: conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"translateText": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.TranslateText(), testConnection),
	}}
}
