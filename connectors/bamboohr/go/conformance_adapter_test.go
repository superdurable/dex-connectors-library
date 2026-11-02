// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/superdurable/dex-connectors-library/connectors/bamboohr/go"
)

const conformanceEndpointPath = "/api/v1"
const conformanceFixturePathPrefix = "/api/v1"

type conformanceBambooHRContext struct{ *bambooHRDexContext }

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	context := &conformanceBambooHRContext{newBambooHRDexContext("conformance")}
	if len(scenario.PriorHeartbeat) != 0 {
		var marker struct {
			Attempt int32 `json:"attempt"`
		}
		if err := json.Unmarshal(scenario.PriorHeartbeat, &marker); err != nil {
			t.Fatal(err)
		}
		context.previousHeartbeat = scenario.PriorHeartbeat
		context.attempt = marker.Attempt + 1
	}
	client, err := bamboohr.New(bamboohr.Config{CompanyDomain: testCompanyDomain}, testCredentialProvider(), bamboohr.WithHTTPClient(transport), bamboohr.WithAPIBaseURL(endpoint))
	if err != nil {
		t.Fatal(err)
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.bambooHRDexContext = context.nextAttempt()
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
