// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hackernews

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

const conformanceEndpointPath = "/v0"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: 300}, nil, WithHTTPClient(transport), withClock(conformanceClock(t, scenario)))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listStoryIDs": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListStoryIDs(), testConnection),
		"getItem":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetItem(), testConnection),
	}}
}
