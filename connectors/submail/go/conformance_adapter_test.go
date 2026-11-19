// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package submail

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
	appID, appKey := "test-app-id", "test-app-key"
	if scenario.Name == "getSMSBalance-defect" {
		appID, appKey = "", ""
	}
	credentials := Credentials{AppID: appID, AppKey: sdkgo.NewSecretString(appKey)}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getSMSBalance": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSMSBalance(), testConnection),
	}}
}
