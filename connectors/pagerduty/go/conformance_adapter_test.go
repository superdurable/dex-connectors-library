// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pagerduty

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"net/http"
	"testing"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}}, WithHTTPClient(transport))
	require.NoError(t, err)
	context := &pagerDutyConformanceContext{}
	if len(scenario.PriorHeartbeat) != 0 {
		context.heartbeat = append(json.RawMessage(nil), scenario.PriorHeartbeat...)
		var marker struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		context.attempt = marker.Attempt + 1
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) { context.attempt = int32(attempt) }, actions: map[string]conformanceOperationInvocation{
		"listIncidents":      newConformanceQueryInvocation(t, scenario.Input, context, client.ListIncidents(), testConnection),
		"getIncident":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetIncident(), testConnection),
		"listIncidentNotes":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListIncidentNotes(), testConnection),
		"listLogEntries":     newConformanceQueryInvocation(t, scenario.Input, context, client.ListLogEntries(), testConnection),
		"getLogEntry":        newConformanceQueryInvocation(t, scenario.Input, context, client.GetLogEntry(), testConnection),
		"getUser":            newConformanceQueryInvocation(t, scenario.Input, context, client.GetUser(), testConnection),
		"updateIncident":     newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateIncident(), testConnection, scenario.CallID),
		"createIncident":     newConformanceMutationInvocation(t, scenario.Input, context, client.CreateIncident(), testConnection, scenario.CallID),
		"createIncidentNote": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateIncidentNote(), testConnection, scenario.CallID),
	}}
}

type pagerDutyConformanceContext struct {
	providerTestContext
	attempt   int32
	heartbeat json.RawMessage
}

func (context *pagerDutyConformanceContext) Attempt() int32 {
	if context.attempt == 0 {
		return 1
	}
	return context.attempt
}
func (context *pagerDutyConformanceContext) RecordHeartbeat(value any) error {
	if value == nil {
		context.heartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}
func (context *pagerDutyConformanceContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
