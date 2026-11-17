// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sell

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v2"
const conformanceFixturePathPrefix = "/v2"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "zendesk-sell", Name: "zendesk-sell"}
	credentials := sdkgo.StaticCredentialProvider[Credentials]{reference: {AccessToken: sdkgo.NewSecretString("test-token")}}
	if scenario.Name == "listLeads-defect" {
		credentials = sdkgo.StaticCredentialProvider[Credentials]{}
	}
	client, err := New(Config{BaseURL: endpoint}, credentials, WithHTTPClient(transport))
	require.NoError(t, err)
	writeContext := newSellWriteContext()
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { writeContext.attempt = int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"listLeads":  newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListLeads(), reference),
		"getLead":    newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetLead(), reference),
		"createLead": newConformanceMutationInvocation(t, scenario.Input, writeContext, client.CreateLead(), reference, scenario.CallID),
		"updateLead": newConformanceMutationInvocation(t, scenario.Input, writeContext, client.UpdateLead(), reference, scenario.CallID),
		"deleteLead": newConformanceMutationInvocation(t, scenario.Input, writeContext, client.DeleteLead(), reference, scenario.CallID),
	}}
}
