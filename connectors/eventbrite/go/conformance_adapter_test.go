// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventbrite

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
	reference := sdkgo.ConnectionRef{Provider: "eventbrite", Name: "eventbrite"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {PrivateToken: sdkgo.NewSecretString("test-token")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listOrganizations": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListOrganizations(), reference),
		"listEvents":        newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.ListEvents(), reference),
		"getAttendee":       newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.GetAttendee(), reference),
	}}
}
