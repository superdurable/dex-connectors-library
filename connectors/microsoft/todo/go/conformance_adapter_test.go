// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todo

import (
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
)

const conformanceEndpointPath = "/v1.0"
const conformanceFixturePathPrefix = "/v1.0"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "microsoft", Name: "microsoft-todo"}
	token := "sample-access-token"
	if scenario.Name == "listTaskLists-defect" {
		token = ""
	}
	credentials := Credentials{OAuthClientID: "sample-client-id", OAuthClientSecret: sdkgo.NewSecretString("sample-client-secret"), AccessToken: sdkgo.NewSecretString(token), RefreshToken: sdkgo.NewSecretString("sample-refresh-token")}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listTaskLists": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTaskLists(), reference),
		"listTasks":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTasks(), reference),
	}}
}
