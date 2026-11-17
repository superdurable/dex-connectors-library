// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickhouse

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
	reference := sdkgo.ConnectionRef{Provider: "clickhouse", Name: "clickhouse"}
	username := "reader"
	if scenario.Name == "listDatabases-defect" {
		username = ""
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {Username: username, Password: sdkgo.NewSecretString("sample-clickhouse-password")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listDatabases": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListDatabases(), reference),
		"listTables":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTables(), reference),
		"listColumns":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListColumns(), reference),
	}}
}
