// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wise

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/api"
const conformanceFixturePathPrefix = "/api"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "wise", Name: "wise"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIToken: sdkgo.NewSecretString("sample-wise-api-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listProfiles":     newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListProfiles(), reference),
		"getProfile":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetProfile(), reference),
		"listRecipients":   newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListRecipients(), reference),
		"getExchangeRates": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetExchangeRates(), reference),
		"getQuote":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetQuote(), reference),
		"createTransfer":   newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateTransfer(), reference, scenario.CallID),
		"cancelTransfer":   newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CancelTransfer(), reference, scenario.CallID),
		"getTransfer":      newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetTransfer(), reference),
	}}
}
