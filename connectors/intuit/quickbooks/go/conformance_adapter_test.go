// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
	"net"
	"net/http"
	"testing"

	quickbooks "github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = "/v3/company/" + testRealmID

// A before-send event represents a dial refusal, which Go classifies by net.OpError.
type conformanceDialTransport struct {
	next     http.RoundTripper
	recorder *conformanceRecorder
}

func (transport conformanceDialTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	index := transport.recorder.requestCount
	response, err := transport.next.RoundTrip(request)
	if err != nil && index < len(transport.recorder.scenario.Exchanges) &&
		transport.recorder.scenario.Exchanges[index].TransportFailure == "before-send" {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: err}
	}
	return response, err
}

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	transport.Transport = conformanceDialTransport{next: transport.Transport, recorder: transport.Transport.(*conformanceRecorder)}
	client := newQuickBooksClient(t, endpoint, quickbooks.WithHTTPClient(transport))
	context := newQuickBooksDexContext("conformance")
	prior := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(raw []byte) { context.previousHeartbeat = raw })
	return &conformanceMappedInvoker{t: t, scenario: scenario,
		before: func(attempt int) {
			if attempt > 1 {
				context.previousHeartbeat = context.recordedHeartbeat
			}
			context.attempt = prior + int32(attempt)
		},
		actions: map[string]conformanceOperationInvocation{
			"findCustomer":   newConformanceQueryInvocation(t, scenario.Input, context, client.FindCustomer(), quickbooksConnection),
			"createCustomer": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateCustomer(), quickbooksConnection, scenario.CallID),
			"createInvoice":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateInvoice(), quickbooksConnection, scenario.CallID),
			"getInvoice":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetInvoice(), quickbooksConnection),
			"listInvoices":   newConformanceQueryInvocation(t, scenario.Input, context, client.ListInvoices(), quickbooksConnection),
			"sendInvoice":    newConformanceMutationInvocation(t, scenario.Input, context, client.SendInvoice(), quickbooksConnection, scenario.CallID),
			"recordPayment":  newConformanceMutationInvocation(t, scenario.Input, context, client.RecordPayment(), quickbooksConnection, scenario.CallID),
		}}
}
