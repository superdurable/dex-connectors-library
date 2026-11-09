// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package heap

import (
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

const conformanceEndpointPath, conformanceFixturePathPrefix = "", ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint, AppID: "12345", MaxResponseBytes: 8}, nil, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"setUserProperties": newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.SetUserProperties(), testConnection, scenario.CallID),
	}}
}
