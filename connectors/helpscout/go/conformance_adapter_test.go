// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout/go"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

type helpScoutConformanceContext struct {
	*stepContext
	attempt int32
}

func (context *helpScoutConformanceContext) Attempt() int32 { return context.attempt }

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, _ string, transport *http.Client) conformanceInvoker {
	t.Helper()
	context := &helpScoutConformanceContext{stepContext: newStepContext("helpscout-conformance"), attempt: 1}
	if len(scenario.PriorHeartbeat) != 0 {
		context.heartbeat = append([]byte(nil), scenario.PriorHeartbeat...)
		var marker struct {
			Attempt int32 `json:"attempt"`
		}
		require.NoError(t, json.Unmarshal(scenario.PriorHeartbeat, &marker))
		context.attempt = marker.Attempt + 1
	}
	client := newTestClient(t, transport, staticCredentials("inert-webhook-secret"), helpscout.Config{})
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.heartbeat = append([]byte(nil), context.heartbeat...)
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"searchConversations": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchConversations(), testConnection),
		"getConversation":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetConversation(), testConnection),
		"replyToConversation": newConformanceMutationInvocation(t, scenario.Input, context, client.ReplyToConversation(), testConnection, scenario.CallID),
		"updateConversation":  newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateConversation(), testConnection, scenario.CallID),
		"findCustomerByEmail": newConformanceQueryInvocation(t, scenario.Input, context, client.FindCustomerByEmail(), testConnection),
	}}
}
