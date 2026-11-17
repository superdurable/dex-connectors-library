// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package apifox

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {AccessToken: sdkgo.NewSecretString("test-secret")},
	}
	client, err := New(Config{}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getProjectAPISummary": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetProjectAPISummary(), testConnection),
	}}
}
