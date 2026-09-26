// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex/sdk-go/dex"
)

// TriggerRef is the stable manifest identity of one connector trigger.
type TriggerRef struct {
	ConnectorID string `json:"connectorId" yaml:"connectorId"`
	TriggerName string `json:"triggerName" yaml:"triggerName"`
}

// Validate checks that the trigger reference uses manifest-compatible identifiers.
func (reference TriggerRef) Validate() error {
	if !connectorIDPattern.MatchString(reference.ConnectorID) {
		return fmt.Errorf("connector ID must be DNS-like and 2-63 characters")
	}
	if !operationIDPattern.MatchString(reference.TriggerName) {
		return fmt.Errorf("trigger name must be lower camel case")
	}
	return nil
}

// TriggerDefinition describes one provider event source declared by a connector manifest.
type TriggerDefinition struct {
	Trigger     TriggerRef `json:"trigger" yaml:"trigger"`
	Description string     `json:"description" yaml:"description"`
}

// Validate checks a trigger definition before a runner starts.
func (definition TriggerDefinition) Validate() error {
	if err := definition.Trigger.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(definition.Description) == "" {
		return fmt.Errorf("trigger description is required")
	}
	return nil
}

// TriggerBindingRef identifies one configured use of a connector trigger.
type TriggerBindingRef struct {
	Connection ConnectionRef `json:"connection" yaml:"connection"`
	Trigger    TriggerRef    `json:"trigger" yaml:"trigger"`
	Name       string        `json:"name" yaml:"name"`
}

// TriggerBindingDefinition exposes one static application binding to tooling.
type TriggerBindingDefinition struct {
	Definition      TriggerDefinition         `json:"definition" yaml:"definition"`
	ConnectionName  string                    `json:"connectionName" yaml:"connectionName"`
	BindingName     string                    `json:"bindingName" yaml:"bindingName"`
	ConfigurationUI *ConnectorConfigurationUI `json:"configurationUI,omitempty" yaml:"configurationUI,omitempty"`
}

// Validate checks the static identity used by Flow Definition tooling.
func (definition TriggerBindingDefinition) Validate() error {
	if err := definition.Definition.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(definition.ConnectionName) == "" || strings.TrimSpace(definition.BindingName) == "" {
		return fmt.Errorf("trigger binding connection name and binding name are required")
	}
	if definition.ConfigurationUI != nil {
		if err := definition.ConfigurationUI.Validate(); err != nil {
			return fmt.Errorf("trigger binding configuration UI: %w", err)
		}
	}
	return nil
}

// MustTriggerBindingDefinition validates static application Trigger metadata.
func MustTriggerBindingDefinition(definition TriggerBindingDefinition) TriggerBindingDefinition {
	if err := definition.Validate(); err != nil {
		panic(err)
	}
	return definition
}

// Validate checks that a binding has a connection, trigger, and stable binding name.
func (binding TriggerBindingRef) Validate() error {
	if err := binding.Connection.Validate(); err != nil {
		return err
	}
	if err := binding.Trigger.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(binding.Name) == "" {
		return fmt.Errorf("trigger binding name is required")
	}
	return nil
}

// TriggerEvent is one provider event with a stable provider identity.
type TriggerEvent[T any] struct {
	ID         string    `json:"eventId"`
	OccurredAt time.Time `json:"occurredAt"`
	Payload    T         `json:"payload"`
}

// TriggerTarget handles one event after the provider transport acknowledges receipt.
type TriggerTarget[T any] interface {
	HandleTrigger(context.Context, TriggerEvent[T]) error
}

// TriggerDeliveryPreparer persists an event before a provider acknowledges it.
type TriggerDeliveryPreparer[T any] interface {
	PrepareTrigger(context.Context, TriggerEvent[T]) error
}

// TriggerDeliveryReplayer redelivers events persisted before a previous process stopped.
type TriggerDeliveryReplayer interface {
	ReplayTriggerDeliveries(context.Context) error
}

// TriggerTargetFunc adapts a function to TriggerTarget.
type TriggerTargetFunc[T any] func(context.Context, TriggerEvent[T]) error

// HandleTrigger handles one external trigger event.
func (target TriggerTargetFunc[T]) HandleTrigger(ctx context.Context, event TriggerEvent[T]) error {
	return target(ctx, event)
}

// PrepareTriggerDelivery asks a durable target to persist an event before provider acknowledgement.
func PrepareTriggerDelivery[T any](ctx context.Context, target TriggerTarget[T], event TriggerEvent[T]) error {
	preparer, ok := target.(TriggerDeliveryPreparer[T])
	if !ok {
		return nil
	}
	return preparer.PrepareTrigger(ctx, event)
}

// TriggerSource owns the provider transport and emits acknowledged events.
type TriggerSource[T any] interface {
	Run(context.Context, TriggerTarget[T]) error
}

// TriggerRunner is a configured long-running connector trigger.
type TriggerRunner interface {
	Run(context.Context) error
	Definition() TriggerDefinition
	Binding() TriggerBindingRef
}

// TriggerConfig configures one provider source and application target.
type TriggerConfig[T any] struct {
	Definition TriggerDefinition
	Binding    TriggerBindingRef
	Source     TriggerSource[T]
	Target     TriggerTarget[T]
}

type triggerRunner[T any] struct {
	definition TriggerDefinition
	binding    TriggerBindingRef
	source     TriggerSource[T]
	target     TriggerTarget[T]
}

// NewTrigger validates and constructs one trigger runner.
func NewTrigger[T any](config TriggerConfig[T]) (TriggerRunner, error) {
	if err := config.Definition.Validate(); err != nil {
		return nil, fmt.Errorf("trigger definition: %w", err)
	}
	if err := config.Binding.Validate(); err != nil {
		return nil, fmt.Errorf("trigger binding: %w", err)
	}
	if config.Binding.Trigger != config.Definition.Trigger {
		return nil, fmt.Errorf("trigger binding does not match its definition")
	}
	if config.Source == nil || config.Target == nil {
		return nil, fmt.Errorf("trigger source and target are required")
	}
	return &triggerRunner[T]{definition: config.Definition, binding: config.Binding, source: config.Source, target: config.Target}, nil
}

// MustNewTrigger constructs a trigger runner or panics for invalid static application wiring.
func MustNewTrigger[T any](config TriggerConfig[T]) TriggerRunner {
	runner, err := NewTrigger(config)
	if err != nil {
		panic(err)
	}
	return runner
}

func (runner *triggerRunner[T]) Run(ctx context.Context) error {
	if replayer, ok := runner.target.(TriggerDeliveryReplayer); ok {
		if err := replayer.ReplayTriggerDeliveries(ctx); err != nil {
			return fmt.Errorf("replay Trigger deliveries: %w", err)
		}
	}
	return runner.source.Run(ctx, runner.target)
}

func (runner *triggerRunner[T]) Definition() TriggerDefinition { return runner.definition }

func (runner *triggerRunner[T]) Binding() TriggerBindingRef { return runner.binding }

// TriggerFactoryConfigMarker identifies generated provider Trigger factory configs.
type TriggerFactoryConfigMarker struct{}

// TriggerBindingFactoryConfigMarker identifies generated static Trigger binding configs.
type TriggerBindingFactoryConfigMarker struct{}

// TriggerFilter decides whether an application accepts a provider event for Dex routing.
// Returning false consumes the event without starting a Flow or invoking an RPC.
// The filter must be deterministic and side-effect free because durable delivery may evaluate it again after a restart.
type TriggerFilter[EVENT any] func(TriggerEvent[EVENT]) bool

// FlowIDResolver maps one provider event to the stable application Flow ID that owns it.
type FlowIDResolver[EVENT any] func(TriggerEvent[EVENT]) string

// FlowInputMapper maps one provider event to a Flow's typed start input.
type FlowInputMapper[EVENT, INPUT any] func(TriggerEvent[EVENT]) INPUT

// RPCInputMapper maps one provider event to an application's typed RPC input.
type RPCInputMapper[EVENT, INPUT any] func(TriggerEvent[EVENT]) INPUT

// NewDexFlowTriggerTarget creates a filtered target that starts a typed Dex Flow.
func NewDexFlowTriggerTarget[EVENT, INPUT any](
	client *dex.Client,
	flow dex.Flow,
	filterEvent TriggerFilter[EVENT],
	resolveFlowID FlowIDResolver[EVENT],
	mapToFlowInput FlowInputMapper[EVENT, INPUT],
) TriggerTarget[EVENT] {
	if client == nil || flow == nil || filterEvent == nil || resolveFlowID == nil || mapToFlowInput == nil {
		panic("Dex client, Flow, Trigger filter, Flow ID resolver, and Flow input mapper are required")
	}
	return TriggerTargetFunc[EVENT](func(ctx context.Context, event TriggerEvent[EVENT]) error {
		if !filterEvent(event) {
			return nil
		}
		flowID := resolveFlowID(event)
		if strings.TrimSpace(flowID) == "" || strings.TrimSpace(event.ID) == "" {
			return fmt.Errorf("Flow trigger requires Flow ID and event ID")
		}
		input := mapToFlowInput(event)
		requestID := event.ID
		_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{RequestID: &requestID})
		var alreadyStarted *dex.FlowAlreadyStartedError
		if errors.As(err, &alreadyStarted) {
			return nil
		}
		return err
	})
}

// NewDexRPCTriggerTarget filters an event before invoking one typed RPC on the resolved Flow.
func NewDexRPCTriggerTarget[EVENT, INPUT, OUTPUT any](
	client *dex.Client,
	rpc dex.RPC[INPUT, OUTPUT],
	filterEvent TriggerFilter[EVENT],
	resolveFlowID FlowIDResolver[EVENT],
	mapToRPCInput RPCInputMapper[EVENT, INPUT],
) TriggerTarget[EVENT] {
	if client == nil || rpc == nil || filterEvent == nil || resolveFlowID == nil || mapToRPCInput == nil {
		panic("Dex client, RPC, Trigger filter, Flow ID resolver, and RPC input mapper are required")
	}
	return TriggerTargetFunc[EVENT](func(ctx context.Context, event TriggerEvent[EVENT]) error {
		if !filterEvent(event) {
			return nil
		}
		flowID := resolveFlowID(event)
		if strings.TrimSpace(flowID) == "" || strings.TrimSpace(event.ID) == "" {
			return fmt.Errorf("RPC trigger requires Flow ID and event ID")
		}
		var output OUTPUT
		return client.InvokeRPC(ctx, flowID, rpc, mapToRPCInput(event), &output)
	})
}
