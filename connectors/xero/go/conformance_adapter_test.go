// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero_test

import (
	"net/http"
	"testing"

	"github.com/superdurable/dex-connectors-library/connectors/xero/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/api.xro/2.0"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	client := newXeroClient(t, endpoint, xero.WithHTTPClient(transport), xero.WithClock(conformanceFixedClock(t, scenario)))
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
