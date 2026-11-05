// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package forms_test

import (
  "net/http"
  "testing"

  "github.com/stretchr/testify/require"
  "github.com/superdurable/dex-connectors-library/connectors/google/forms/go"
  "github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
  client, err := forms.New(forms.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[forms.Credentials]{
    formsConnection: {AccessToken: sdkgo.NewSecretString("one-use-token")},
  }, forms.WithHTTPClient(transport))
  require.NoError(t, err)
  return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
    "getForm": newConformanceQueryInvocation(t, scenario.Input, newDexContext("get-form"), client.GetForm(), formsConnection),
    "listResponses": newConformanceQueryInvocation(t, scenario.Input, newDexContext("list-responses"), client.ListResponses(), formsConnection),
    "getResponse": newConformanceQueryInvocation(t, scenario.Input, newDexContext("get-response"), client.GetResponse(), formsConnection),
  }}
}
