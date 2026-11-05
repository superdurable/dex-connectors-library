// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = companyPath

type conformanceBodyTransport struct{ next http.RoundTripper }

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	// The recorder reads every request body, including GETs whose Body is nil.
	transport = &http.Client{Transport: conformanceBodyTransport{transport.Transport}}
	client, err := quickbooks.New(quickbooks.Config{}, staticCredentials(testIDToken(t, `"realmid":"`+testRealmID+`"`)),
		quickbooks.WithHTTPClient(transport), quickbooks.WithLocalProviderURL(endpoint))
	require.NoError(t, err)
	context := newQuickBooksDexContext("conformance")
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
		"findCustomer":   newConformanceQueryInvocation(t, scenario.Input, context, client.FindCustomer(), quickbooksConnection),
		"createCustomer": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateCustomer(), quickbooksConnection, scenario.CallID),
		"createInvoice":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateInvoice(), quickbooksConnection, scenario.CallID),
		"getInvoice":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetInvoice(), quickbooksConnection),
		"listInvoices":   newConformanceQueryInvocation(t, scenario.Input, context, client.ListInvoices(), quickbooksConnection),
		"sendInvoice":    newConformanceMutationInvocation(t, scenario.Input, context, client.SendInvoice(), quickbooksConnection, scenario.CallID),
		"recordPayment":  newConformanceMutationInvocation(t, scenario.Input, context, client.RecordPayment(), quickbooksConnection, scenario.CallID),
	}}
}

func (transport conformanceBodyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body == nil {
		copied := request.Clone(request.Context())
		copied.Body = http.NoBody
		request = copied
	}
	return transport.next.RoundTrip(request)
}
