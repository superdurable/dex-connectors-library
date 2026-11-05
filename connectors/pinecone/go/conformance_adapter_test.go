// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pinecone

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
	connection := sdkgo.ConnectionRef{Provider: "pinecone", Name: "pinecone"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		connection: {APIKey: sdkgo.NewSecretString("pinecone-test-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"upsertVectors": newConformanceMutationInvocation(t, scenario.Input, testContext{}, client.UpsertVectors(), connection, scenario.CallID),
		"queryVectors":  newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.QueryVectors(), connection),
	}}
}
