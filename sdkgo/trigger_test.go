// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex/sdk-go/dex"
)

type triggerSource struct {
	event sdkgo.TriggerEvent[string]
}

type filterTestFlow struct {
	dex.FlowDefaults
}

func (*filterTestFlow) GetSteps() []dex.StepDef { return nil }

func (*filterTestFlow) GetRPCs() []dex.RPCDef { return nil }

func (*filterTestFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

func (*filterTestFlow) ReceiveEvent(_ dex.Context, input int) (*dex.RPCResult[string], error) {
	return &dex.RPCResult[string]{Output: fmt.Sprintf("%d", input)}, nil
}

func (source triggerSource) Run(ctx context.Context, target sdkgo.TriggerTarget[string]) error {
	return target.HandleTrigger(ctx, source.event)
}

func TestTriggerRunnerPreservesDefinitionBindingAndEvent(t *testing.T) {
	definition := sdkgo.TriggerDefinition{
		Trigger:     sdkgo.TriggerRef{ConnectorID: "slack", TriggerName: "channelThreadCreated"},
		Description: "Receive a matching Slack thread.",
	}
	binding := sdkgo.TriggerBindingRef{
		Connection: sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"},
		Trigger:    definition.Trigger, Name: "approval-start",
	}
	event := sdkgo.TriggerEvent[string]{ID: "Ev123", OccurredAt: time.Unix(42, 0), Payload: "hello"}
	var delivered sdkgo.TriggerEvent[string]
	runner, err := sdkgo.NewTrigger(sdkgo.TriggerConfig[string]{
		Definition: definition,
		Binding:    binding,
		Source:     triggerSource{event: event},
		Target: sdkgo.TriggerTargetFunc[string](func(_ context.Context, received sdkgo.TriggerEvent[string]) error {
			delivered = received
			return nil
		}),
	})
	require.NoError(t, err)
	require.NoError(t, runner.Run(context.Background()))
	require.Equal(t, event, delivered)
	require.Equal(t, definition, runner.Definition())
	require.Equal(t, binding, runner.Binding())
}

func TestTriggerRunnerRejectsMismatchedBinding(t *testing.T) {
	_, err := sdkgo.NewTrigger(sdkgo.TriggerConfig[string]{
		Definition: sdkgo.TriggerDefinition{
			Trigger:     sdkgo.TriggerRef{ConnectorID: "slack", TriggerName: "channelThreadCreated"},
			Description: "Receive a channel thread.",
		},
		Binding: sdkgo.TriggerBindingRef{
			Connection: sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"},
			Trigger:    sdkgo.TriggerRef{ConnectorID: "slack", TriggerName: "threadReplyCreated"},
			Name:       "approval-start",
		},
		Source: triggerSource{},
		Target: sdkgo.TriggerTargetFunc[string](func(context.Context, sdkgo.TriggerEvent[string]) error { return nil }),
	})
	require.ErrorContains(t, err, "does not match")
}

func TestTriggerBindingDefinitionRequiresStaticNames(t *testing.T) {
	definition := sdkgo.TriggerBindingDefinition{
		Definition: sdkgo.TriggerDefinition{
			Trigger:     sdkgo.TriggerRef{ConnectorID: "slack", TriggerName: "channelThreadCreated"},
			Description: "Receive a channel thread.",
		},
	}
	require.ErrorContains(t, definition.Validate(), "connection name")
	definition.ConnectionName = "workspace"
	definition.BindingName = "approval-start"
	require.NoError(t, definition.Validate())
}

func TestDexFlowTriggerTargetConsumesFilteredEventBeforeRouting(t *testing.T) {
	resolverCalled := false
	inputMapperCalled := false
	logs := testsupport.NewLogRecorder()
	target := sdkgo.NewDexFlowTriggerTarget(
		&dex.Client{},
		&filterTestFlow{},
		func(sdkgo.TriggerEvent[string]) bool { return false },
		func(sdkgo.TriggerEvent[string]) string {
			resolverCalled = true
			return "flow-id"
		},
		func(sdkgo.TriggerEvent[string]) string {
			inputMapperCalled = true
			return "input"
		},
		sdkgo.WithTriggerLogger(logs.Logger().With("binding", "approval-start")),
	)

	require.NoError(t, target.HandleTrigger(context.Background(), sdkgo.TriggerEvent[string]{ID: "event-id", Payload: "SENTINEL-MESSAGE-TEXT"}))
	require.False(t, resolverCalled)
	require.False(t, inputMapperCalled)
	requireSingleFilteredRecord(t, logs, "flow_start", "approval-start")
}

// requireSingleFilteredRecord checks the only record a Dex target logs for a filtered event.
func requireSingleFilteredRecord(t *testing.T, logs *testsupport.LogRecorder, target string, binding string) {
	t.Helper()
	records := logs.Records()
	require.Len(t, records, 1)
	require.Equal(t, slog.LevelInfo, records[0].Level)
	require.Equal(t, "trigger event skipped: filtered", records[0].Message)
	require.Equal(t, map[string]string{"binding": binding, "target": target, "event_id": "event-id"}, records[0].Attrs)
	require.NotContains(t, logs.Text(), "SENTINEL")
}

func TestDexRPCTriggerTargetConsumesFilteredEventBeforeRouting(t *testing.T) {
	resolverCalled := false
	inputMapperCalled := false
	logs := testsupport.NewLogRecorder()
	flow := &filterTestFlow{}
	target := sdkgo.NewDexRPCTriggerTarget(
		&dex.Client{},
		flow.ReceiveEvent,
		func(sdkgo.TriggerEvent[string]) bool { return false },
		func(sdkgo.TriggerEvent[string]) string {
			resolverCalled = true
			return "flow-id"
		},
		func(sdkgo.TriggerEvent[string]) int {
			inputMapperCalled = true
			return 42
		},
		sdkgo.WithTriggerLogger(logs.Logger().With("binding", "approval-reply")),
	)

	require.NoError(t, target.HandleTrigger(context.Background(), sdkgo.TriggerEvent[string]{ID: "event-id", Payload: "SENTINEL-MESSAGE-TEXT"}))
	require.False(t, resolverCalled)
	require.False(t, inputMapperCalled)
	requireSingleFilteredRecord(t, logs, "rpc", "approval-reply")
}

// TestTriggerRPCRequestIDMatchesTypeScriptVectors: triggerRPCRequestId in sdkts/src/trigger.ts printed each
// expected ID for the same RPC name and event ID.
func TestTriggerRPCRequestIDMatchesTypeScriptVectors(t *testing.T) {
	testCases := []struct {
		name      string
		rpcName   string
		eventID   string
		requestID string
	}{
		{name: "documented example", rpcName: "receiveReply", eventID: "Ev1", requestID: "trigger-rpc-kDFFz9I4ELDtzXkmvRTzlA"},
		{name: "Go method name", rpcName: "Approve", eventID: "Ev1", requestID: "trigger-rpc-NCT4ExQx0Ahaf4r71ej-1g"},
		{name: "wire name", rpcName: "send_user_message", eventID: "Ev1", requestID: "trigger-rpc-nvt7sgyIgs8au6Su_i3G_A"},
		{name: "Slack event ID", rpcName: "send_user_message", eventID: "1710000200.000200", requestID: "trigger-rpc-jULspDTupwRV1mIcS03qUg"},
		{name: "provider event ID", rpcName: "ReceiveEnvelopeEvent", eventID: "evt_0123456789abcdef", requestID: "trigger-rpc-N-9RTMPf6sXlUYgoeYwLfg"},
		{name: "multibyte UTF-8", rpcName: "recordLedgerEntry", eventID: "café-事件-😀", requestID: "trigger-rpc-cVlFUH2QCxK31tF2KjqSUA"},
		{name: "empty event ID", rpcName: "receiveReply", eventID: "", requestID: "trigger-rpc-hBjdrS1VKDIhGHGRhqXIxw"},
		// TypeScript hashes the unpaired surrogate in "a\ud800b" as U+FFFD; Go hashes each invalid byte so.
		{name: "invalid UTF-8 byte", rpcName: "receiveReply", eventID: "a\xffb", requestID: "trigger-rpc--DMh9mmLQmtdCEEqMlYxWA"},
		{name: "replacement character", rpcName: "receiveReply", eventID: "a�b", requestID: "trigger-rpc--DMh9mmLQmtdCEEqMlYxWA"},
		// encoding/json stores the truncated sequence as two U+FFFD, as TypeScript hashes "x��y".
		{name: "truncated UTF-8 sequence", rpcName: "receiveReply", eventID: "x\xe2\x82y", requestID: "trigger-rpc-8RndI-WQcs_0IiUPVanorg"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			requestID, err := sdkgo.TriggerRPCRequestID(testCase.rpcName, testCase.eventID)
			require.NoError(t, err)
			require.Equal(t, testCase.requestID, requestID)
			require.True(t, strings.HasPrefix(requestID, sdkgo.TriggerRPCRequestIDPrefix))
			require.Len(t, requestID, len(sdkgo.TriggerRPCRequestIDPrefix)+22)
		})
	}
}

func TestTriggerRPCRequestIDRejectsUnusableRPCName(t *testing.T) {
	for _, rpcName := range []string{"", "receive\x00Reply"} {
		requestID, err := sdkgo.TriggerRPCRequestID(rpcName, "Ev1")
		require.EqualError(t, err, "RPC request ID requires an RPC name without NUL characters")
		require.Empty(t, requestID)
	}
}

// TestTriggerRPCRequestIDSurvivesDurableInboxEncoding: an event ID with invalid UTF-8 keeps its request ID
// through the inbox's JSON round trip.
func TestTriggerRPCRequestIDSurvivesDurableInboxEncoding(t *testing.T) {
	event := sdkgo.TriggerEvent[string]{ID: "Ev\xe2\x82\xff1", Payload: "hello"}
	encoded, err := json.Marshal(event)
	require.NoError(t, err)
	var replayed sdkgo.TriggerEvent[string]
	require.NoError(t, json.Unmarshal(encoded, &replayed))
	require.NotEqual(t, event.ID, replayed.ID)
	live, err := sdkgo.TriggerRPCRequestID("Approve", event.ID)
	require.NoError(t, err)
	afterReplay, err := sdkgo.TriggerRPCRequestID("Approve", replayed.ID)
	require.NoError(t, err)
	require.Equal(t, live, afterReplay)
}

func TestDexRPCTriggerTargetRejectsUnusableRPCName(t *testing.T) {
	flow := &filterTestFlow{}
	for _, rpcName := range []string{"", "  ", "receive\x00Event"} {
		require.PanicsWithValue(t, "RPC trigger RPC name must contain a non-blank character and no NUL characters", func() {
			sdkgo.NewDexRPCTriggerTarget(&dex.Client{}, flow.ReceiveEvent,
				func(sdkgo.TriggerEvent[string]) bool { return true },
				func(sdkgo.TriggerEvent[string]) string { return "flow-id" },
				func(sdkgo.TriggerEvent[string]) int { return 42 },
				sdkgo.WithTriggerRPCName(rpcName))
		}, "rpcName %q", rpcName)
	}
}
