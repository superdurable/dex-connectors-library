// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudflare

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/client/v4"
const conformanceFixturePathPrefix = "/client/v4"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	token := "test-apitoken"
	if scenario.Name == "listZones-defect" {
		token = ""
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIToken: sdkgo.NewSecretString(token)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := testContext{}
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listZones":        newConformanceQueryInvocation(t, scenario.Input, context, client.ListZones(), testConnection),
		"purgeCacheByURLs": newConformanceMutationInvocation(t, scenario.Input, context, client.PurgeCacheByURLs(), testConnection, scenario.CallID),
	}}
}
