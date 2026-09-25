// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type triggerSource struct {
	event sdkgo.TriggerEvent[string]
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
