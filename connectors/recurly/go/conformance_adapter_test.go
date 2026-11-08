// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recurly

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = "/api"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "recurly", Name: "recurly"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-recurly-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getAccount": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAccount(), reference),
	}}
}
