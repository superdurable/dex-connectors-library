// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tingyun

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
	credentials := Credentials{AccessToken: sdkgo.NewSecretString("access-secret")}
	if scenario.Name == "listApplications-defect" {
		credentials.AccessToken = sdkgo.NewSecretString("")
	}
	client, err := New(Config{APIHost: "https://wukong2.tingyun.com/"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listApplications": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListApplications(), testConnection),
	}}
}
