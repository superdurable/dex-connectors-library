// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/superdurable/dex-connectors-library/connectors/reamaze/go"
)

const conformanceEndpointPath = "/api/v1"
const conformanceFixturePathPrefix = "/api/v1"

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := reamaze.New(reamaze.Config{Brand: testBrand}, testCredentialProvider(), reamaze.WithAPIBaseURL(endpoint), reamaze.WithHTTPClient(transport))
	if err != nil {
		t.Fatal(err)
	}
	context := newReamazeDexContext("conformance")
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
			context.heartbeatCount = 0
			context.attempt = int32(attempt)
		}
	}, actions: map[string]conformanceOperationInvocation{
		"searchConversations": newConformanceQueryInvocation(t, scenario.Input, context, client.SearchConversations(), reamazeConnection),
		"getConversation":     newConformanceQueryInvocation(t, scenario.Input, context, client.GetConversation(), reamazeConnection),
		"createConversation":  newConformanceMutationInvocation(t, scenario.Input, context, client.CreateConversation(), reamazeConnection, scenario.CallID),
		"updateConversation":  newConformanceMutationInvocation(t, scenario.Input, context, client.UpdateConversation(), reamazeConnection, scenario.CallID),
		"replyToConversation": newConformanceMutationInvocation(t, scenario.Input, context, client.ReplyToConversation(), reamazeConnection, scenario.CallID),
		"findContactByEmail":  newConformanceQueryInvocation(t, scenario.Input, context, client.FindContactByEmail(), reamazeConnection),
	}}
}
