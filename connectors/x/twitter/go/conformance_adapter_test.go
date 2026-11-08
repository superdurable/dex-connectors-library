// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package x

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/2"
const conformanceFixturePathPrefix = "/2"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "x-twitter", Name: "x"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString("sample-x-access-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getPost": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetPost(), reference),
	}}
}
