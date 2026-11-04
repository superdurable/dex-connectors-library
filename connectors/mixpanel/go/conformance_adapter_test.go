// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mixpanel

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
	client, err := New(Config{BaseURL: endpoint, ExportBaseURL: endpoint + "/api/2.0", ProjectID: 12345}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {ServiceAccountUsername: sdkgo.NewSecretString("service-user"), ServiceAccountSecret: sdkgo.NewSecretString("service-secret")},
	})
	require.NoError(t, err)
	client.httpClient = transport
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"trackEvent":   newConformanceMutationInvocation(t, scenario.Input, providerContext{}, client.TrackEvent(), testConnection, scenario.CallID),
		"exportEvents": newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.ExportEvents(), testConnection),
	}}
}
