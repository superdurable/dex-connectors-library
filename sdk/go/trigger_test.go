// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connector_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

type triggerSource struct {
	event connector.TriggerEvent[string]
}

func (source triggerSource) Run(ctx context.Context, target connector.TriggerTarget[string]) error {
	return target.HandleTrigger(ctx, source.event)
}

func TestTriggerRunnerPreservesDefinitionBindingAndEvent(t *testing.T) {
	definition := connector.TriggerDefinition{
		Trigger:     connector.TriggerRef{ConnectorID: "slack", TriggerName: "channelThreadCreated"},
		Description: "Receive a matching Slack thread.",
	}
	binding := connector.TriggerBindingRef{
		Connection: connector.ConnectionRef{Provider: "slack", Name: "workspace"},
		Trigger:    definition.Trigger, Name: "approval-start",
	}
	event := connector.TriggerEvent[string]{ID: "Ev123", OccurredAt: time.Unix(42, 0), Payload: "hello"}
	var delivered connector.TriggerEvent[string]
	runner, err := connector.NewTrigger(connector.TriggerConfig[string]{
		Definition: definition,
		Binding:    binding,
		Source:     triggerSource{event: event},
		Target: connector.TriggerTargetFunc[string](func(_ context.Context, received connector.TriggerEvent[string]) error {
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
	_, err := connector.NewTrigger(connector.TriggerConfig[string]{
		Definition: connector.TriggerDefinition{
			Trigger:     connector.TriggerRef{ConnectorID: "slack", TriggerName: "channelThreadCreated"},
			Description: "Receive a channel thread.",
		},
		Binding: connector.TriggerBindingRef{
			Connection: connector.ConnectionRef{Provider: "slack", Name: "workspace"},
			Trigger:    connector.TriggerRef{ConnectorID: "slack", TriggerName: "threadReplyCreated"},
			Name:       "approval-start",
		},
		Source: triggerSource{},
		Target: connector.TriggerTargetFunc[string](func(context.Context, connector.TriggerEvent[string]) error { return nil }),
	})
	require.ErrorContains(t, err, "does not match")
}

func TestTriggerBindingDefinitionRequiresStaticNames(t *testing.T) {
	definition := connector.TriggerBindingDefinition{
		Definition: connector.TriggerDefinition{
			Trigger:     connector.TriggerRef{ConnectorID: "slack", TriggerName: "channelThreadCreated"},
			Description: "Receive a channel thread.",
		},
	}
	require.ErrorContains(t, definition.Validate(), "connection name")
	definition.ConnectionName = "workspace"
	definition.BindingName = "approval-start"
	require.NoError(t, definition.Validate())
}
