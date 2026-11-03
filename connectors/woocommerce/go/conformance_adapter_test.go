// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package woocommerce

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/shop"
const conformanceFixturePathPrefix = "/shop"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "woocommerce", Name: "woocommerce"}
	client, err := New(Config{StoreURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {ConsumerKey: sdkgo.NewSecretString("ck_test"), ConsumerSecret: sdkgo.NewSecretString("cs_test")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listOrders": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOrders(), reference),
		"getOrder":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetOrder(), reference),
	}}
}
