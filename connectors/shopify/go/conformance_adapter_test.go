// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shopify

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/admin/api/2026-10"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	_ = endpoint // Shopify's API host is fixed; the recorder intercepts its transport.
	client, err := New(Config{ShopSubdomain: "test-shop"}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {AccessToken: sdkgo.NewSecretString("test-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getOrder":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOrder(), testConnection),
		"listOrders": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOrders(), testConnection),
	}}
}
