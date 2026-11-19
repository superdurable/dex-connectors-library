// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package weiling

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{
		AppID: sdkgo.NewSecretString("REDACTED_APP_ID"),
		AppSecret: sdkgo.NewSecretString("REDACTED_APP_SECRET"),
	}
	if scenario.Name == "listDepartments-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listDepartments": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListDepartments(), testConnection),
	}}
}
