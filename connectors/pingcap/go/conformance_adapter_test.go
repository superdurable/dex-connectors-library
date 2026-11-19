// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pingcap

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{PublicKey: sdkgo.NewSecretString(testPublicKey), PrivateKey: sdkgo.NewSecretString(testPrivateKey)}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "listProjects-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), nonceReaderOption{reader: newConformanceSyntheticRandomReader(scenario.RandomSeed)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{"listProjects": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProjects(), testConnection)}}
}
