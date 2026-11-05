// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/pipedrive/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client := newAPITokenClient(t, endpoint, pipedrive.WithHTTPClient(transport), pipedrive.WithClock(conformanceFixedClock(t, scenario)))
	context := newStepDexContext("conformance")
	initialAttempt := int32(1)
	if len(scenario.PriorHeartbeat) != 0 {
		var wrapped struct {
			Value   json.RawMessage `json:"value"`
			Attempt int32           `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &wrapped))
		if len(wrapped.Value) > 0 {
			context.heartbeat = wrapped.Value
		} else {
			context.heartbeat = scenario.PriorHeartbeat
		}
		initialAttempt = wrapped.Attempt + 1
		context.attempt = initialAttempt
	}
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		context.attempt = initialAttempt + int32(attempt) - 1
	}, actions: map[string]conformanceOperationInvocation{
		"searchObjects": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchObjects(), pipedriveConnection),
		"getObject":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetObject(), pipedriveConnection),
		"listObjects":   newConformanceQueryInvocation(t, scenario.Input, context, client.ListObjects(), pipedriveConnection),
		"upsertObject":  newConformanceMutationInvocation(t, scenario.Input, context, client.UpsertObject(), pipedriveConnection, scenario.CallID),
		"createObject":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateObject(), pipedriveConnection, scenario.CallID),
		"updateObject":  newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateObject(), pipedriveConnection, scenario.CallID),
	}}
}
