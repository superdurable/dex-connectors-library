// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package netease

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{AppKey: sdkgo.NewSecretString(testAppKey), AppSecret: sdkgo.NewSecretString(testAppSecret)}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "getUserProfile-oversized" {
		config.MaxResponseBytes = 4
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), signingInputsOption{now: conformanceFixedClock(t, scenario), nonceReader: newConformanceSyntheticRandomReader(scenario.RandomSeed)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{"getUserProfile": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetUserProfile(), testConnection)}}
}
