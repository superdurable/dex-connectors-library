// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package qingflow

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
	token := "private-test-key"
	if scenario.Name == "listApplications-defect" {
		token = ""
	}
	credentials := Credentials{AccessToken: sdkgo.NewSecretString(token)}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listApplications": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListApplications(), testConnection),
	}}
}
