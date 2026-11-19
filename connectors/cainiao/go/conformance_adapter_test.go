// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cainiao

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/router/rest"
const conformanceFixturePathPrefix = ""

var conformanceRequestTime = time.Date(2026, 10, 22, 7, 26, 40, 0, time.UTC)

type conformanceClockOption struct{}

func (conformanceClockOption) applyClientOption(client *Client) {
	client.clock = func() time.Time { return conformanceRequestTime }
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {
		AppKey: "123456", AppSecret: sdkgo.NewSecretString("secretKey"), SessionKey: sdkgo.NewSecretString("sessionToken"),
	}}
	if scenario.Name == "queryWaybillTrace-missing-credentials" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "queryWaybillTrace-oversized" {
		config.MaxResponseBytes = 8
	}
	client, err := New(config, credentials, WithHTTPClient(transport), conformanceClockOption{})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"queryWaybillTrace": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.QueryWaybillTrace(), testConnection),
	}}
}
