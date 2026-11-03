// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr_test

import (
	"encoding/json"
	"net"
	"net/http"
	"testing"

	"github.com/superdurable/dex-connectors-library/connectors/bamboohr/go"
)

const conformanceEndpointPath = "/api/v1"
const conformanceFixturePathPrefix = "/api/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	if scenario.Name == "addEmployee-transport-before-send" {
		wrapped := *transport
		wrapped.Transport = beforeSendConformanceTransport{transport.Transport}
		transport = &wrapped
	}
	client, err := bamboohr.New(bamboohr.Config{CompanyDomain: testCompanyDomain}, testCredentialProvider(),
		bamboohr.WithAPIBaseURL(endpoint), bamboohr.WithHTTPClient(transport))
	if err != nil {
		t.Fatal(err)
	}
	context := newBambooHRDexContext("conformance")
	if len(scenario.PriorHeartbeat) != 0 {
		context.previousHeartbeat = append(json.RawMessage(nil), scenario.PriorHeartbeat...)
		var marker struct {
			Attempt int32 `json:"attempt"`
		}
		if err := json.Unmarshal(scenario.PriorHeartbeat, &marker); err != nil {
			t.Fatal(err)
		}
		context.attempt = marker.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.previousHeartbeat = append(json.RawMessage(nil), context.recordedHeartbeat...)
			context.recordedHeartbeat = nil
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"getEmployee":         newConformanceQueryInvocation(t, scenario.Input, context, client.GetEmployee(), bambooHRConnection),
		"findEmployeeByEmail": newConformanceQueryInvocation(t, scenario.Input, context, client.FindEmployeeByEmail(), bambooHRConnection),
		"listEmployeeChanges": newConformanceQueryInvocation(t, scenario.Input, context, client.ListEmployeeChanges(), bambooHRConnection),
		"listTimeOffRequests": newConformanceQueryInvocation(t, scenario.Input, context, client.ListTimeOffRequests(), bambooHRConnection),
		"updateEmployee":      newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateEmployee(), bambooHRConnection, scenario.CallID),
		"addEmployee":         newConformanceMutationInvocation(t, scenario.Input, context, client.AddEmployee(), bambooHRConnection, scenario.CallID),
	}}
}

type beforeSendConformanceTransport struct{ next http.RoundTripper }

func (transport beforeSendConformanceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.next.RoundTrip(request)
	if err != nil {
		return nil, &net.OpError{Op: "dial", Err: err}
	}
	return response, nil
}
