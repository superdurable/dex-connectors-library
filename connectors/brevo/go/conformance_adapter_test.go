// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package brevo

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v3"
const conformanceFixturePathPrefix = ""

type conformanceHTTPClientOption struct{ client *http.Client }

func (option conformanceHTTPClientOption) applyClientOption(client *Client) {
	client.httpClient = option.client
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIKey: sdkgo.NewSecretString("brevo-test-key")},
	}, conformanceHTTPClientOption{client: transport})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"createOrUpdateContact": newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.CreateOrUpdateContact(), testConnection, scenario.CallID),
		"getContact":            newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.GetContact(), testConnection),
	}}
}
