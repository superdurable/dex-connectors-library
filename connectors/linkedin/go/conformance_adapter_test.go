// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedin

import (
  "net/http"
  "testing"

  "github.com/stretchr/testify/require"
  "github.com/superdurable/dex-connectors-library/connectors/linkedin/go/internal/testsupport"
  "github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v2/userinfo"
const conformanceFixturePathPrefix = ""

var conformanceConnection = sdkgo.ConnectionRef{Provider: "linkedin", Name: "signup"}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
  credentials := sdkgo.StaticCredentialProvider[Credentials]{
    conformanceConnection: {AccessToken: sdkgo.NewSecretString("one-use-token")},
  }
  if scenario.Name == "missing-credential" { credentials = sdkgo.StaticCredentialProvider[Credentials]{} }
  client, err := New(Config{UserInfoURL: endpoint}, credentials, WithHTTPClient(transport))
  require.NoError(t, err)
  return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
    "getAuthenticatedProfile": newConformanceQueryInvocation(t, scenario.Input, testsupport.NewDexContext("linkedin-conformance", "get-authenticated-profile"), client.GetAuthenticatedProfile(), conformanceConnection),
  }}
}
