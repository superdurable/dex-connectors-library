// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"context"
	"fmt"
	"log/slog"
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
