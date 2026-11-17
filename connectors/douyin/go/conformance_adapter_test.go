// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package douyin

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type conformanceClockOption struct{}

func (conformanceClockOption) applyClientOption(client *Client) {
	client.clock = func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, chinaTime) }
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	config := Config{}
	if scenario.Name == "getBehaviorSummary-token-oversize" {
		config.MaxResponseBytes = 8
	}
	credentials := Credentials{ClientKey: "tt-test-app", ClientSecret: sdkgo.NewSecretString("test-client-secret")}
	if scenario.Name == "getBehaviorSummary-missing-secret" {
		credentials = Credentials{ClientKey: "tt-test-app"}
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), conformanceClockOption{})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getBehaviorSummary": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetBehaviorSummary(), testConnection),
	}}
}
