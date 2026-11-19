// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taosdata

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
	credentials := Credentials{CloudURL: sdkgo.NewSecretString("https://gw.us-central-1.gcp.cloud.tdengine.com"), CloudToken: sdkgo.NewSecretString("secret-cloud-token")}
	if scenario.Name == "listDatabases-defect" {
		credentials.CloudURL = sdkgo.NewSecretString("")
	}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listDatabases": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListDatabases(), testConnection),
	}}
}
