// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rippling

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "rippling", Name: "rippling"}
	config := Config{BaseURL: endpoint}
	if strings.HasSuffix(scenario.Name, "-smallLimit") {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIToken: sdkgo.NewSecretString("sample-rippling-api-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getWorker":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetWorker(), reference),
		"listWorkers": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListWorkers(), reference),
	}}
}
