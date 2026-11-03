// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package unsplash

import (
  "net/http"
  "testing"

  "github.com/stretchr/testify/require"
  "github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
  client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
    testConnection: {AccessKey: sdkgo.NewSecretString("unsplash-test-key")},
  }, conformanceHTTPClientOption{transport})
  require.NoError(t, err)
  return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
    "searchPhotos": newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.SearchPhotos(), testConnection),
    "getPhoto": newConformanceQueryInvocation(t, scenario.Input, providerContext{}, client.GetPhoto(), testConnection),
  }}
}

type conformanceHTTPClientOption struct{ client *http.Client }

func (option conformanceHTTPClientOption) applyClientOption(client *Client) { client.httpClient = option.client }
