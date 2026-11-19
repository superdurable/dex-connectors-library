// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package moka

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
	credentials := Credentials{APIKey: sdkgo.NewSecretString("secret-api-key")}
	if scenario.Name == "listJobFunctions-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{OrgID: "orgid-test", Mode: "social"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listJobFunctions": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListJobFunctions(), testConnection),
	}}
}
