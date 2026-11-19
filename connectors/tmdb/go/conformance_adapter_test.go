// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tmdb

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/3"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"searchMovies": newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.SearchMovies(), testConnection),
		"getMovie": newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetMovie(), testConnection),
	}}
}
