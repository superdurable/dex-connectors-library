// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fit2cloud

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
	credentials := Credentials{APIKey: sdkgo.NewSecretString(testAPIKey)}
	if scenario.Name == "listPosts-defect" {
		credentials = Credentials{}
	}
	configuration := Config{BaseURL: "https://halo.example.com"}
	if scenario.Name == "listPosts-oversized" {
		configuration.MaxResponseBytes = 16
	}
	client, err := New(configuration, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listPosts": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListPosts(), testConnection),
	}}
}
