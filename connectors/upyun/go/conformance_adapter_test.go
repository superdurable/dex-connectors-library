// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package upyun

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
		Operator: "upyun", Password: sdkgo.NewSecretString("password"),
	}}
	if scenario.Name == "getServiceUsage-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	config := Config{BaseURL: endpoint, Bucket: "bucket"}
	if scenario.Name == "getServiceUsage-oversized" {
		config.MaxResponseBytes = 3
	}
	client, err := New(config, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	client.now = func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC) }
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getServiceUsage": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetServiceUsage(), testConnection),
	}}
}
