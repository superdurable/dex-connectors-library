// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package posthog

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
	client, err := New(Config{CaptureURL: endpoint, AppURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {
			ProjectToken: sdkgo.NewSecretString("phc_synthetic-token"),
			PersonalAPIKey: sdkgo.NewSecretString("phx_synthetic-key"),
		},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"captureEvent": newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.CaptureEvent(), testConnection, scenario.CallID),
		"listInsights": newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.ListInsights(), testConnection),
	}}
}
