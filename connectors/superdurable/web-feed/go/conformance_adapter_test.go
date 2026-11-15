// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := New(Config{}, nil, conformanceTransportOption{transport: transport}, clockTestOption{clock: conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"readFeed":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ReadFeed(), testConnection),
		"discoverFeeds":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.DiscoverFeeds(), testConnection),
		"readSitemap":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ReadSitemap(), testConnection),
		"readPageMetadata": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ReadPageMetadata(), testConnection),
	}}
}

// conformanceTransportOption replays recorded exchanges in place of the address policy, which dials real addresses.
type conformanceTransportOption struct{ transport *http.Client }

func (option conformanceTransportOption) applyClientOption(options *clientOptions) {
	options.newPolicyClient = func(_ *http.Client, requestTimeout time.Duration) (*http.Client, error) {
		return providerhttp.NewProviderHTTPClient(option.transport, requestTimeout), nil
	}
}
