// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qihoo360

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
	credentials := Credentials{APIKey: sdkgo.NewSecretString("REDACTED_QIHOO_API_KEY")}
	if scenario.Name == "generateText-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"generateText": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GenerateText(), testConnection),
	}}
}
