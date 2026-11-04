// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sentry

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
  client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
    testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")},
  }, WithHTTPClient(transport))
  require.NoError(t, err)
  return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
    "listIssues": newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListIssues(), testConnection),
    "getIssue": newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetIssue(), testConnection),
    "updateIssueStatus": newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.UpdateIssueStatus(), testConnection, scenario.CallID),
  }}
}
