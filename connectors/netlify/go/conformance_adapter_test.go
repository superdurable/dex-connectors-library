// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package netlify

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, conformanceCredentials{unavailable: scenario.Name == "sites-missing-credentials"}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listSites":   newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListSites(), testConnection),
		"listDeploys": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListDeploys(), testConnection),
	}}
}

type conformanceCredentials struct{ unavailable bool }

func (provider conformanceCredentials) Resolve(_ sdkgo.Call) (Credentials, error) {
	if provider.unavailable {
		return Credentials{}, errors.New("credentials unavailable")
	}
	return Credentials{AccessToken: sdkgo.NewSecretString("test-accesstoken")}, nil
}
