// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datadog

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
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{
		testConnection: {APIKey: sdkgo.NewSecretString("test-apikey"), ApplicationKey: sdkgo.NewSecretString("test-applicationkey")},
	}, WithHTTPClient(transport))
	require.NoError(t, err)
	createContext := singledispatchtest.NewContext("datadog-conformance", "create-monitor")
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if scenario.Operation == "createMonitor" && attempt > 1 {
			createContext.AdvanceAttempt()
		}
	}, actions: map[string]conformanceOperationInvocation{
		"createMonitor": newConformanceMutationInvocation(t, scenario.Input, createContext, client.CreateMonitor(), testConnection, scenario.CallID),
		"queryMetrics":  newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.QueryMetrics(), testConnection),
		"listMonitors":  newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.ListMonitors(), testConnection),
		"getMonitor":    newConformanceQueryInvocation(t, scenario.Input, testContext{}, client.GetMonitor(), testConnection),
		"updateMonitor": newConformanceMutationInvocation(t, scenario.Input, testContext{}, client.UpdateMonitor(), testConnection, scenario.CallID),
		"deleteMonitor": newConformanceMutationInvocation(t, scenario.Input, testContext{}, client.DeleteMonitor(), testConnection, scenario.CallID),
	}}
}
