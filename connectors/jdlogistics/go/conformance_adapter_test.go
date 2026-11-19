// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jdlogistics

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {
		AppKey: "app123", AppSecret: sdkgo.NewSecretString("secretKey"),
		AccessToken: sdkgo.NewSecretString("token123"), CustomerCode: "010K123",
	}}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "queryWaybillTrace-oversized" {
		config.MaxResponseBytes = 5
	}
	client, err := New(config, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	client.clock = func() time.Time {
		return time.Date(2025, 1, 2, 3, 4, 5, 0, time.FixedZone("CST", 8*3600))
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"queryWaybillTrace": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.QueryWaybillTrace(), testConnection),
	}}
}
