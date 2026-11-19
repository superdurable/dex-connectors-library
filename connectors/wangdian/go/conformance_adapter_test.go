// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wangdian

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{SID: sdkgo.NewSecretString(testSID), AppKey: sdkgo.NewSecretString(testAppKey), AppSecret: sdkgo.NewSecretString(testAppSecret)}
	if scenario.Name == "listShops-defect" {
		credentials = Credentials{}
	}
	configuration := Config{BaseURL: "https://openapi.huice.com"}
	if scenario.Name == "listShops-oversized" {
		configuration.MaxResponseBytes = 16
	}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	client.now = func() time.Time { return time.Unix(1731510000, 0) }
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listShops": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListShops(), testConnection),
	}}
}
