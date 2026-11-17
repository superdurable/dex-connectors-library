// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package singledispatch

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/conformancefixture"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// markerConformance is the cross-language single-dispatch fixture.
type markerConformance struct {
	Markers             []Marker             `json:"markers"`
	CheckpointScenarios []checkpointScenario `json:"checkpointScenarios"`
}

// checkpointScenario replays fenced calls of one Step attempt in order.
// An absent previousHeartbeat means the previous attempt recorded none.
type checkpointScenario struct {
	Name              string                   `json:"name"`
	Attempt           int32                    `json:"attempt"`
	PreviousHeartbeat json.RawMessage          `json:"previousHeartbeat,omitempty"`
	Calls             []checkpointScenarioCall `json:"calls"`
	Heartbeats        []json.RawMessage        `json:"heartbeats"`
}

// checkpointScenarioCall decides, then dispatches when Dispatch is set and the decision permits it.
type checkpointScenarioCall struct {
	CallID            sdkgo.CallID `json:"callId"`
	MaxSends          int32        `json:"maxSends"`
	Dispatch          bool         `json:"dispatch"`
	ProviderID        string       `json:"providerId,omitempty"`
	Decision          string       `json:"decision"`
	MaximumPriorSends int32        `json:"maximumPriorSends"`
}

const (
	conformanceCallA = sdkgo.CallID(conformancefixture.ExampleCallID)
	conformanceCallB = sdkgo.CallID("0b9d3c3e-6a51-5f0e-9a43-7d2f1f7c2b10")
	conformanceCallC = sdkgo.CallID("9e0f6c21-3b7a-5d4e-8f12-6a5b4c3d2e1f")
)

func TestConformanceMarkers(t *testing.T) {
	callID := sdkgo.CallID(conformancefixture.ExampleCallID)
	conformancefixture.Check(t, "single-dispatch-markers.json", markerConformance{
		Markers: []Marker{
			{CallID: callID, Attempt: 1, State: StateNotSent},
			{CallID: callID, Attempt: 2, State: StateClaimed},
			{CallID: callID, Attempt: 3, State: StateCanResend},
			{CallID: callID, Attempt: 4, State: StateClaimed, ProviderID: "object-42"},
			{CallID: callID, Attempt: 5, State: StateClaimed, SendCount: 2},
			{CallID: callID, Attempt: 6, State: StateNotSent, SendCount: 2},
			{CallID: callID, Attempt: 7, State: StateCanResend, SendCount: 2},
			{CallID: callID, Attempt: 8, State: StateClaimed, ProviderID: "object-43", SendCount: 3},
		},
		CheckpointScenarios: recordCheckpointScenarios(t),
	})
}

func recordCheckpointScenarios(t *testing.T) []checkpointScenario {
	t.Helper()
	entries := func(markers ...Marker) checkpointHeartbeat {
		heartbeat := checkpointHeartbeat{SingleDispatchMarkers: map[sdkgo.CallID]Marker{}}
		for _, marker := range markers {
			heartbeat.SingleDispatchMarkers[marker.CallID] = marker
		}
		return heartbeat
	}
	call := func(callID sdkgo.CallID, maxSends int32, shouldDispatch bool) checkpointScenarioCall {
		return checkpointScenarioCall{CallID: callID, MaxSends: maxSends, Dispatch: shouldDispatch}
	}
	scenarios := []struct {
		name     string
		attempt  int32
		previous any
		calls    []checkpointScenarioCall
	}{
		{name: "two calls on attempt one both send", attempt: 1, calls: []checkpointScenarioCall{call(conformanceCallA, 1, true), call(conformanceCallB, 1, true)}},
		{
			name: "attempt two runs call B before call A", attempt: 2,
			previous: entries(Marker{CallID: conformanceCallA, Attempt: 1, State: StateClaimed}, Marker{CallID: conformanceCallB, Attempt: 1, State: StateCanResend}),
			calls: []checkpointScenarioCall{
				call(conformanceCallB, 1, true),
				{CallID: conformanceCallA, MaxSends: 3, Dispatch: true, ProviderID: "object-42"},
			},
		},
		{
			name: "attempt two keeps call A's entry when only call B sends", attempt: 2,
			previous: entries(Marker{CallID: conformanceCallA, Attempt: 1, State: StateClaimed}, Marker{CallID: conformanceCallB, Attempt: 1, State: StateNotSent}),
			calls:    []checkpointScenarioCall{call(conformanceCallB, 1, true), call(conformanceCallA, 1, true)},
		},
		{
			name: "legacy own marker is ignored", attempt: 2,
			previous: Marker{CallID: conformanceCallA, Attempt: 1, State: StateNotSent},
			calls:    []checkpointScenarioCall{call(conformanceCallA, 1, true)},
		},
		{
			name: "legacy own marker is ignored under bounded resend", attempt: 2,
			previous: Marker{CallID: conformanceCallA, Attempt: 1, State: StateClaimed, ProviderID: "object-42", SendCount: 1},
			calls:    []checkpointScenarioCall{call(conformanceCallA, 3, true)},
		},
		{
			name: "legacy foreign marker is ignored", attempt: 2,
			previous: Marker{CallID: conformanceCallB, Attempt: 1, State: StateNotSent},
			calls:    []checkpointScenarioCall{call(conformanceCallA, 3, true)},
		},
		{name: "garbage string is ignored", attempt: 2, previous: "not a checkpoint", calls: []checkpointScenarioCall{call(conformanceCallA, 3, true)}},
		{name: "garbage on attempt one is ignored", attempt: 1, previous: []any{1, 2}, calls: []checkpointScenarioCall{call(conformanceCallA, 1, true)}},
		{name: "unknown object shape is ignored", attempt: 2, previous: map[string]any{"progress": 3}, calls: []checkpointScenarioCall{call(conformanceCallA, 1, true)}},
		{
			name: "unreadable map entries are dropped", attempt: 3,
			previous: map[string]any{checkpointMarkersField: map[string]any{
				string(conformanceCallA): map[string]any{"callId": string(conformanceCallA), "attempt": 2, "state": "notSent", "extra": true},
				string(conformanceCallB): map[string]any{"callId": string(conformanceCallC), "attempt": 2, "state": "notSent"},
				string(conformanceCallC): map[string]any{"callId": string(conformanceCallC), "attempt": "two", "state": "notSent"},
			}},
			calls: []checkpointScenarioCall{call(conformanceCallA, 3, true)},
		},
		{
			name: "absent entry on attempt two uses the attempt-one bound", attempt: 2,
			previous: entries(Marker{CallID: conformanceCallB, Attempt: 1, State: StateClaimed}),
			calls:    []checkpointScenarioCall{call(conformanceCallA, 3, true), call(conformanceCallC, 1, true)},
		},
		{
			name: "impossible own entry fails closed", attempt: 2,
			previous: entries(Marker{CallID: conformanceCallA, Attempt: 1, State: StateClaimed, SendCount: 2}),
			calls:    []checkpointScenarioCall{call(conformanceCallA, 3, true)},
		},
	}
	recorded := make([]checkpointScenario, 0, len(scenarios))
	for _, scenario := range scenarios {
		ctx := singledispatchtest.NewContext("flow-conformance", "step-conformance")
		var previous json.RawMessage
		if scenario.previous != nil {
			encoded, err := json.Marshal(scenario.previous)
			require.NoError(t, err)
			previous = encoded
			require.NoError(t, ctx.SetPersistedCheckpoint(previous))
		}
		for ctx.Attempt() < scenario.attempt {
			ctx.AdvanceAttempt()
		}
		calls := make([]checkpointScenarioCall, 0, len(scenario.calls))
		for _, scenarioCall := range scenario.calls {
			fence, err := NewFence(sdkgo.Call{Context: ctx, ID: scenarioCall.CallID}, time.Minute, WithMaxSends(scenarioCall.MaxSends))
			require.NoError(t, err, scenario.name)
			decision := fence.Decide()
			scenarioCall.Decision = conformanceDecisionName(t, decision.Kind)
			scenarioCall.MaximumPriorSends = decision.MaximumPriorSends
			if scenarioCall.Dispatch && isSendDecision(decision.Kind) {
				require.NoError(t, fence.Dispatch(func(context.Context) error { return nil }), scenario.name)
				if scenarioCall.ProviderID != "" {
					require.NoError(t, fence.RecordProviderID(scenarioCall.ProviderID), scenario.name)
				}
			}
			calls = append(calls, scenarioCall)
		}
		heartbeats := make([]json.RawMessage, 0)
		for _, frame := range ctx.Heartbeats() {
			heartbeats = append(heartbeats, frame)
		}
		recorded = append(recorded, checkpointScenario{Name: scenario.name, Attempt: scenario.attempt, PreviousHeartbeat: previous, Calls: calls, Heartbeats: heartbeats})
	}
	return recorded
}

func conformanceDecisionName(t *testing.T, kind DecisionKind) string {
	t.Helper()
	switch kind {
	case SendAllowed:
		return "sendAllowed"
	case ReadBackRequired:
		return "readBackRequired"
	case FailClosed:
		return "failClosed"
	case ReadBackThenResendAllowed:
		return "readBackThenResendAllowed"
	default:
		t.Fatalf("unknown decision kind %d", kind)
		return ""
	}
}
