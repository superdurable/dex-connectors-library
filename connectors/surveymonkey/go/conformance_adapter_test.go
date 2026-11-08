// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package surveymonkey

import (
 "net/http"
 "testing"

 "github.com/stretchr/testify/require"
 "github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v3"
const conformanceFixturePathPrefix = "/v3"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
 t.Helper()
 reference := sdkgo.ConnectionRef{Provider:"surveymonkey",Name:"surveymonkey"}
 client,err:=New(Config{BaseURL:endpoint},sdkgo.StaticCredentialProvider[Credentials]{reference:{AccessToken:sdkgo.NewSecretString("test-token")}},WithHTTPClient(transport))
 require.NoError(t,err)
 return &conformanceMappedInvoker{t:t,scenario:scenario,actions:map[string]conformanceOperationInvocation{
  "listSurveys":newConformanceQueryInvocation(t,scenario.Input,queryContext{},client.ListSurveys(),reference),
  "getResponse":newConformanceQueryInvocation(t,scenario.Input,queryContext{},client.GetResponse(),reference),
 }}
}
