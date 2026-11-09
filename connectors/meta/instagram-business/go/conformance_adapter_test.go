// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package instagrambusiness

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v25.0"
const conformanceFixturePathPrefix = "/v25.0"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "instagram-business", Name: "instagram-business"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: {UserAccessToken: sdkgo.NewSecretString("test-instagram-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listMedia": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListMedia(), reference),
	}}
}
