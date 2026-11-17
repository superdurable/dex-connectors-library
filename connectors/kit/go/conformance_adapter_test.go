// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kit

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v4"
const conformanceFixturePathPrefix = "/v4"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "kit", Name: "kit"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString("test-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listSubscribers":         newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListSubscribers(), reference),
		"getSubscriber":           newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetSubscriber(), reference),
		"upsertSubscriber":        newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertSubscriber(), reference, scenario.CallID),
		"listTags":                newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListTags(), reference),
		"createTag":               newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateTag(), reference, scenario.CallID),
		"tagSubscriber":           newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.TagSubscriber(), reference, scenario.CallID),
		"removeTagFromSubscriber": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.RemoveTagFromSubscriber(), reference, scenario.CallID),
		"listCustomFields":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListCustomFields(), reference),
		"createCustomField":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.CreateCustomField(), reference, scenario.CallID),
		"updateCustomField":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpdateCustomField(), reference, scenario.CallID),
		"deleteCustomField":       newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteCustomField(), reference, scenario.CallID),
	}}
}
