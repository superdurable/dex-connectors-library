// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bmob

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
	config := Config{BaseURL: endpoint}
	if scenario.Name == "countClassObjects-oversized" {
		config.MaxResponseBytes = 16
	}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {
			ApplicationID: sdkgo.NewSecretString(testApplicationID),
			RESTAPIKey: sdkgo.NewSecretString(testRESTAPIKey),
		},
	}
	if scenario.Name == "countClassObjects-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(config, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"countClassObjects": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.CountClassObjects(), testConnection),
	}}
}
