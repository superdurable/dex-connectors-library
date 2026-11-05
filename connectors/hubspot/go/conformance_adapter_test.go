// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hubspot_test

import (
  "net/http"
  "testing"

  "github.com/stretchr/testify/require"
  "github.com/superdurable/dex-connectors-library/connectors/hubspot/go"
  "github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
  t.Helper()
  client, err := hubspot.New(hubspot.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[hubspot.Credentials]{
    hubspotConnection: {AuthMethodID: hubspot.PrivateAppTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(testAccessToken)},
  }, hubspot.WithHTTPClient(transport))
  require.NoError(t, err)
  context := newStepDexContext("conformance")
  return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
    "searchObjects": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchObjects(), hubspotConnection),
    "getObject": newConformanceQueryInvocation(t, scenario.Input, context, client.GetObject(), hubspotConnection),
    "upsertObject": newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertObject(), hubspotConnection, scenario.CallID),
    "updateObject": newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateObject(), hubspotConnection, scenario.CallID),
  }}
}
