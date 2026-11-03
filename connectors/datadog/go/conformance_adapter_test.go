// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datadog

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIKey: sdkgo.NewSecretString("test-apikey"), ApplicationKey: sdkgo.NewSecretString("test-applicationkey")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"queryMetrics": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.QueryMetrics(), testConnection),
		"listMonitors": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListMonitors(), testConnection),
	}}
}
