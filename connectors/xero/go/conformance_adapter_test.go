// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	config := xero.Config{}
	var credentials xero.CredentialSource = customConnectionCredentials()
	if strings.HasPrefix(scenario.Name, "oauth-") {
		config.Organisation = testTenantID
		if strings.Contains(scenario.Name, "named-tenant") {
			config.Organisation = "Maple Florist"
		}
		credentials = sdkgo.StaticCredentialProvider[xero.Credentials]{xeroConnection: {
			AuthMethodID: xero.OAuthAuthMethodID, ClientID: testClientIDPrefix + testClientIDSuffix,
			ClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken),
			RefreshToken: sdkgo.NewSecretString("refresh-token"),
		}}
	}
	client, err := xero.New(config, credentials, xero.WithLocalProviderURL(endpoint), xero.WithHTTPClient(transport), xero.WithClock(conformanceFixedClock(t, scenario)))
	require.NoError(t, err)
	context := newXeroDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"listContacts":       newConformanceQueryInvocation(t, scenario.Input, context, client.ListContacts(), xeroConnection),
		"findContactByEmail": newConformanceQueryInvocation(t, scenario.Input, context, client.FindContactByEmail(), xeroConnection),
		"getInvoice":         newConformanceQueryInvocation(t, scenario.Input, context, client.GetInvoice(), xeroConnection),
		"listInvoices":       newConformanceQueryInvocation(t, scenario.Input, context, client.ListInvoices(), xeroConnection),
		"createInvoice":      newConformanceMutationInvocation(t, scenario.Input, context, client.CreateInvoice(), xeroConnection, scenario.CallID),
		"recordPayment":      newConformanceMutationInvocation(t, scenario.Input, context, client.RecordPayment(), xeroConnection, scenario.CallID),
	}}
}
