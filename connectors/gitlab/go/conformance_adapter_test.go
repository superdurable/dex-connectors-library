// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitlab

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
    testConnection: {AccessToken: sdkgo.NewSecretString("test-accesstoken")},
  }, WithHTTPClient(transport))
  require.NoError(t, err)
  return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
    "getProject": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.GetProject(), testConnection),
    "listIssues": newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListIssues(), testConnection),
    "updateIssueLabels": newConformanceMutationInvocation(t, scenario.Input, testContext{}, client.UpdateIssueLabels(), testConnection, scenario.CallID),
  }}
}
