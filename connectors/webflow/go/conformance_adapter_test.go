// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webflow

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
)

const conformanceEndpointPath = "/v2"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	reference := sdkgo.ConnectionRef{Provider: "webflow", Name: "webflow"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: {SiteToken: sdkgo.NewSecretString("test-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listCollectionItems": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListCollectionItems(), reference),
		"getCollectionItem":   newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.GetCollectionItem(), reference),
	}}
}
