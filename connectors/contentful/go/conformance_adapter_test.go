// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contentful

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "contentful", Name: "contentful"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {DeliveryToken: sdkgo.NewSecretString("test-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getEntry": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.GetEntry(), reference),
		"listEntries": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListEntries(), reference),
		"getAsset": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.GetAsset(), reference),
		"listAssets": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListAssets(), reference),
		"getContentType": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.GetContentType(), reference),
		"listContentTypes": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListContentTypes(), reference),
		"getSpace": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.GetSpace(), reference),
		"listLocales": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListLocales(), reference),
	}}
}
