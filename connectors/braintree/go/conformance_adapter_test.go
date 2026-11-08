// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package braintree

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
	reference := sdkgo.ConnectionRef{Provider: "braintree", Name: "braintree"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {PublicKey: "sample-public-key", PrivateKey: sdkgo.NewSecretString("sample-private-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getTransaction": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTransaction(), reference),
	}}
}
