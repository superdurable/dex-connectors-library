// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tiktok

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/open_api/v1.3"
const conformanceFixturePathPrefix = "/open_api/v1.3"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "tiktok-business", Name: "tiktok-business"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString("sample-tiktok-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listCampaigns": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCampaigns(), reference),
	}}
}
