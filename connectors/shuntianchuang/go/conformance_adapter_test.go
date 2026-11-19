// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shuntianchuang

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
	key := "sk-test-secret"
	if scenario.Name == "listModels-defect" {
		key = "bad key"
	}
	credentials := Credentials{APIKey: sdkgo.NewSecretString(key)}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listModels": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListModels(), testConnection),
	}}
}
