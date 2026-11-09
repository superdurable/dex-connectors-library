// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google-contacts", Name: "google-contacts"}
	credentials := Credentials{
		OAuthClientID: "test-client", OAuthClientSecret: sdkgo.NewSecretString("test-client-secret"),
		AccessToken: sdkgo.NewSecretString("test-contacts-token"), RefreshToken: sdkgo.NewSecretString("test-refresh-token"),
	}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{reference: credentials}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := singledispatchtest.NewContext("contacts-conformance", "provider-step")
	return &conformanceMappedInvoker{t: t, scenario: scenario,
		before: func(attempt int) {
			if attempt > 1 {
				context.AdvanceAttempt()
			}
		},
		actions: map[string]conformanceOperationInvocation{
			"listContacts":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListContacts(), reference),
			"getContact":    newConformanceQueryInvocation(t, scenario.Input, context, client.GetContact(), reference),
			"createContact": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateContact(), reference, scenario.CallID),
		}}
}
