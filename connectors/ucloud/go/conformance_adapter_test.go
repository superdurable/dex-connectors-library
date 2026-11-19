// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ucloud

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
	credentials := Credentials{PublicKey: sdkgo.NewSecretString("test-public-key"), PrivateKey: sdkgo.NewSecretString("test-private-key")}
	if scenario.Name == "getRegion-defect" {
		credentials = Credentials{}
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(&http.Client{Transport: conformanceRootPathTransport{next: transport.Transport}}))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getRegion": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetRegion(), testConnection),
	}}
}

type conformanceRootPathTransport struct {
	next http.RoundTripper
}

func (transport conformanceRootPathTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	observedRequest := request.Clone(request.Context())
	observedRequest.URL.Path = "/"
	return transport.next.RoundTrip(observedRequest)
}
