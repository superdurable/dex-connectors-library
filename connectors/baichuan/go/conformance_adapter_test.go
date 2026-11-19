// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package baichuan

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}}
	if scenario.Name == "listKnowledgeBases-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "listKnowledgeBases-oversized" {
		config.MaxResponseBytes = 30
	}
	client, err := New(config, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listKnowledgeBases": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListKnowledgeBases(), testConnection),
	}}
}
