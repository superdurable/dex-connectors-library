// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package polyv

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
	credentials := Credentials{AppID: "testappid", AppSecret: sdkgo.NewSecretString(testAppSecret)}
	if scenario.Name == "listChannels-defect" {
		credentials = Credentials{}
	}
	config := Config{}
	if scenario.Name == "listChannels-oversized" {
		config.MaxResponseBytes = 8
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), clockOption{now: conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{"listChannels": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListChannels(), testConnection)}}
}
