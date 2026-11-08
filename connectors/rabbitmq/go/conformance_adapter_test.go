// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rabbitmq

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
	reference := sdkgo.ConnectionRef{Provider: "rabbitmq", Name: "rabbitmq"}
	client, err := New(Config{ManagementURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {Username: "observer", Password: sdkgo.NewSecretString("sample-password")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getQueueMetrics": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetQueueMetrics(), reference),
	}}
}
