// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package getui

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{AppID: "test-app", AppKey: sdkgo.NewSecretString("test-appkey"), MasterSecret: sdkgo.NewSecretString("test-mastersecret")}
	if scenario.Name == "getDailyPushReport-defect" {
		credentials = Credentials{}
	}
	config := Config{}
	if scenario.Name == "getDailyPushReport-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), clockOption{now: conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{"getDailyPushReport": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetDailyPushReport(), testConnection)}}
}
