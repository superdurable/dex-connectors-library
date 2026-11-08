// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package insightly

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v3.1"
const conformanceFixturePathPrefix = "/v3.1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "insightly", Name: "insightly"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}}
	if scenario.Name == "listContacts-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listContacts": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListContacts(), reference),
		"getContact":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetContact(), reference),
	}}
}
