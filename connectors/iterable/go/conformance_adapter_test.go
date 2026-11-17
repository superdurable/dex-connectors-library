// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package iterable

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
	reference := sdkgo.ConnectionRef{Provider: "iterable", Name: "iterable"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("sample-iterable-api-key")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"getUser":                 newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetUser(), reference),
		"getUserByID":             newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetUserByID(), reference),
		"upsertUser":              newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UpsertUser(), reference, scenario.CallID),
		"deleteUser":              newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.DeleteUser(), reference, scenario.CallID),
		"trackEvent":              newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.TrackEvent(), reference, scenario.CallID),
		"subscribeUserToList":     newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.SubscribeUserToList(), reference, scenario.CallID),
		"unsubscribeUserFromList": newConformanceMutationInvocation(t, scenario.Input, queryContext{}, client.UnsubscribeUserFromList(), reference, scenario.CallID),
	}}
}
