// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package algolia

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/1"
const conformanceFixturePathPrefix = "/1"

type conformanceTransportOption struct{ transport *http.Client }

func (option conformanceTransportOption) applyClientOption(client *Client) { client.httpClient = option.transport }

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {ApplicationID: "TESTAPP", APIKey: sdkgo.NewSecretString("algolia-test-key")},
	}, conformanceTransportOption{transport})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"search": newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.Search(), testConnection),
		"saveObject": newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.SaveObject(), testConnection, scenario.CallID),
	}}
}
