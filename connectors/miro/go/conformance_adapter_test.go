// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package miro

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v2"
const conformanceFixturePathPrefix = "/v2"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "miro", Name: "miro"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString("sample-miro-access-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getBoard": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetBoard(), reference),
	}}
}
