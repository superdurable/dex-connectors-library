// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newsapi

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v2"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"searchArticles": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.SearchArticles(), testConnection),
	}}
}
