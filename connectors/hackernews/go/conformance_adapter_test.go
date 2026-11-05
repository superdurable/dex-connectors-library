// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hackernews

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
	connection := sdkgo.ConnectionRef{Provider: "hacker-news", Name: "public"}
	client, err := New(Config{BaseURL: endpoint}, nil, WithHTTPClient(transport), conformanceClockOption{conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listStoryIDs": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListStoryIDs(), connection),
		"getItem":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetItem(), connection),
	}}
}

type conformanceClockOption struct{ clock func() time.Time }

func (option conformanceClockOption) applyClientOption(client *Client) { client.clock = option.clock }
