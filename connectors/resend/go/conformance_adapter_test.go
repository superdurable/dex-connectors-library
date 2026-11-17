// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

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
	connection := sdkgo.ConnectionRef{Provider: "resend", Name: "resend"}
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{connection: {APIKey: sdkgo.NewSecretString("test-secret-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"sendEmail":    newConformanceMutationInvocation(t, scenario.Input, providerTestContext{}, client.SendEmail(), connection, scenario.CallID),
		"getEmail":     newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetEmail(), connection),
		"listEmails":   newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListEmails(), connection),
		"getContact":   newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetContact(), connection),
		"listContacts": newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListContacts(), connection),
		"getDomain":    newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.GetDomain(), connection),
		"listDomains":  newConformanceQueryInvocation(t, scenario.Input, providerTestContext{}, client.ListDomains(), connection),
	}}
}
