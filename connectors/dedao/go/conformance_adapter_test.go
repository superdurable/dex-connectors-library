// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dedao

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	config := Config{}
	if scenario.Name == "getNoteTaskStatus-oversize" {
		config.MaxResponseBytes = 16
	}
	credentials := Credentials{APIKey: sdkgo.NewSecretString("gk_live_test-secret"), ClientID: sdkgo.NewSecretString("cli_test-client")}
	if scenario.Name == "getNoteTaskStatus-missing-key" {
		credentials = Credentials{}
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getNoteTaskStatus": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetNoteTaskStatus(), testConnection),
	}}
}
