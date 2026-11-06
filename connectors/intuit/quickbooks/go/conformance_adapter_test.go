// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
    "net/http"
    "testing"

    quickbooks "github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/v3/company/" + testRealmID

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
    client := newQuickBooksClient(t, endpoint, quickbooks.WithHTTPClient(transport))
    context := newQuickBooksDexContext("conformance")
    return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{
        "findCustomer": newConformanceQueryInvocation(t, scenario.Input, context, client.FindCustomer(), quickbooksConnection),
        "createCustomer": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateCustomer(), quickbooksConnection, scenario.CallID),
        "createInvoice": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateInvoice(), quickbooksConnection, scenario.CallID),
        "getInvoice": newConformanceQueryInvocation(t, scenario.Input, context, client.GetInvoice(), quickbooksConnection),
        "listInvoices": newConformanceQueryInvocation(t, scenario.Input, context, client.ListInvoices(), quickbooksConnection),
        "sendInvoice": newConformanceMutationInvocation(t, scenario.Input, context, client.SendInvoice(), quickbooksConnection, scenario.CallID),
        "recordPayment": newConformanceMutationInvocation(t, scenario.Input, context, client.RecordPayment(), quickbooksConnection, scenario.CallID),
    }}
}
