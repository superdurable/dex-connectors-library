// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qince

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""
const conformanceMessageID = "11111111111111111111111111111111"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{OpenID: testOpenID, AppKey: sdkgo.NewSecretString(testAppKey)}
	if scenario.Name == "listDepartments-defect" {
		credentials = Credentials{}
	}
	configuration := Config{BaseURL: "https://openapi.region2.qince.com"}
	if scenario.Name == "listDepartments-oversized" {
		configuration.MaxResponseBytes = 16
	}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	client.messageID = func() (string, error) { return conformanceMessageID, nil }
	client.now = func() time.Time { return time.UnixMilli(1731510000000) }
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listDepartments": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListDepartments(), testConnection),
	}}
}
