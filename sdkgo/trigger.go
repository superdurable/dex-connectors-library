// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/internal/triggerlog"
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
// HandleTrigger returns nil when it consumes the event, an UndeliverableTriggerError when no retry can
// deliver it, or another error to keep the event pending for a retry.
type TriggerTarget[T any] interface {
	HandleTrigger(context.Context, TriggerEvent[T]) error
}

// TriggerDeliveryPreparer persists an event before a provider acknowledges it.
// A prepared event stays pending until the target consumes it by returning nil or an
// UndeliverableTriggerError.
type TriggerDeliveryPreparer[T any] interface {
	PrepareTrigger(context.Context, TriggerEvent[T]) error
}

// TriggerDeliveryReplayer redelivers events persisted before a previous process stopped.
// ReplayTriggerDeliveries retries target failures instead of returning them. It returns nil after every
// event pending at the call has been consumed, ctx.Err() when ctx ends first, or an error when the
// persisted events cannot be read.
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
// Run calls PrepareTriggerDelivery before acknowledging an event, retries a retryable error (for example
// with DeliverTrigger or on its next poll), and treats an UndeliverableTriggerError as consumed rather
// than stopping later deliveries. A TriggerRunner passes Run a target that already returns nil for
// undeliverable events; sources must use PrepareTriggerDelivery instead of asserting target interfaces.
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
	// Logger receives the runner's records, such as an undeliverable event it consumes. Nil uses
	// slog.Default() as of each record.
	Logger *slog.Logger
}

type triggerRunner[T any] struct {
	definition TriggerDefinition
	binding    TriggerBindingRef
	source     TriggerSource[T]
	target     TriggerTarget[T]
	logger     *slog.Logger
}

// NewTrigger validates and constructs one trigger runner.
// The runner's Run replays a TriggerDeliveryReplayer target first. It then runs the source with a
// target that consumes UndeliverableTriggerError results and forwards PrepareTriggerDelivery. That
// target logs each undeliverable event it consumes to config.Logger with the binding's identity.
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
	return &triggerRunner[T]{
		definition: config.Definition, binding: config.Binding, source: config.Source, target: config.Target, logger: config.Logger,
	}, nil
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
	return runner.source.Run(ctx, undeliverableConsumingTarget[T]{
		target: runner.target, log: triggerlog.New(runner.logger, triggerBindingAttrs(runner.binding)...),
	})
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
// The event ID is the Flow-start request ID, so a duplicate start returns nil. An empty Flow ID or event
// ID, or an input that cannot be encoded, returns an UndeliverableTriggerError. Every other error is
// returned unchanged for a retry, including a start that the Dex Server rejects as invalid, such as a
// Step option below the server's configured minimum, so the event replays once the Flow is fixed.
// It logs an INFO "trigger event skipped: filtered" record when the filter rejects an event and a DEBUG
// "trigger event delivered" record when Dex starts the Flow; pass WithTriggerLogger to choose the logger.
// The caller that consumes or retries a returned error logs it.
func NewDexFlowTriggerTarget[EVENT, INPUT any](
	client *dex.Client,
	flow dex.Flow,
	filterEvent TriggerFilter[EVENT],
	resolveFlowID FlowIDResolver[EVENT],
	mapToFlowInput FlowInputMapper[EVENT, INPUT],
	options ...TriggerOption,
) TriggerTarget[EVENT] {
	if client == nil || flow == nil || filterEvent == nil || resolveFlowID == nil || mapToFlowInput == nil {
		panic("Dex client, Flow, Trigger filter, Flow ID resolver, and Flow input mapper are required")
	}
	log := triggerlog.New(resolveTriggerOptions(options).logger, slog.String("target", "flow_start"))
	return TriggerTargetFunc[EVENT](func(ctx context.Context, event TriggerEvent[EVENT]) error {
		if !filterEvent(event) {
			log.Info(ctx, "trigger event skipped: filtered", slog.String("event_id", event.ID))
			return nil
		}
		flowID := resolveFlowID(event)
		if strings.TrimSpace(flowID) == "" || strings.TrimSpace(event.ID) == "" {
			return MarkTriggerUndeliverable(fmt.Errorf("Flow trigger requires Flow ID and event ID"))
		}
		input := mapToFlowInput(event)
		requestID := event.ID
		_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{RequestID: &requestID})
		var alreadyStarted *dex.FlowAlreadyStartedError
		if err == nil || errors.As(err, &alreadyStarted) {
			log.Debug(ctx, "trigger event delivered",
				slog.String("event_id", event.ID), slog.String("flow_id", flowID), slog.Bool("duplicate", err != nil))
			return nil
		}
		return classifyDexTriggerError(err)
	})
}

// NewDexRPCTriggerTarget filters an event before invoking one typed RPC on the resolved Flow.
// It returns an UndeliverableTriggerError when the Flow has closed or was never started, when the RPC
// handler returns the error from MarkTriggerUndeliverable as its outermost error, when the input cannot
// be encoded, or when the Flow ID or event ID is empty. The target cannot tell an event that arrived
// before its Flow started from one whose Flow never existed, so deliver a Flow's start before its RPC
// events. A response that cannot be decoded after Dex applied the RPC returns nil because the target
// discards the output. Every other error is returned unchanged for a retry, including other handler
// errors and a handler that returns another Flow's Dex error. Dex does not deduplicate retried RPCs, so
// the RPC must treat a repeated event ID as a duplicate.
// It logs an INFO "trigger event skipped: filtered" record when the filter rejects an event, a DEBUG
// "trigger event delivered" record when Dex applies the RPC, and a WARN record when Dex applied the RPC
// but its response cannot be decoded; pass WithTriggerLogger to choose the logger. The caller that
// consumes or retries a returned error logs it.
func NewDexRPCTriggerTarget[EVENT, INPUT, OUTPUT any](
	client *dex.Client,
	rpc dex.RPC[INPUT, OUTPUT],
	filterEvent TriggerFilter[EVENT],
	resolveFlowID FlowIDResolver[EVENT],
	mapToRPCInput RPCInputMapper[EVENT, INPUT],
	options ...TriggerOption,
) TriggerTarget[EVENT] {
	if client == nil || rpc == nil || filterEvent == nil || resolveFlowID == nil || mapToRPCInput == nil {
		panic("Dex client, RPC, Trigger filter, Flow ID resolver, and RPC input mapper are required")
	}
	log := triggerlog.New(resolveTriggerOptions(options).logger, slog.String("target", "rpc"))
	return TriggerTargetFunc[EVENT](func(ctx context.Context, event TriggerEvent[EVENT]) error {
		if !filterEvent(event) {
			log.Info(ctx, "trigger event skipped: filtered", slog.String("event_id", event.ID))
			return nil
		}
		flowID := resolveFlowID(event)
		if strings.TrimSpace(flowID) == "" || strings.TrimSpace(event.ID) == "" {
			return MarkTriggerUndeliverable(fmt.Errorf("RPC trigger requires Flow ID and event ID"))
		}
		var output OUTPUT
		err := client.InvokeRPC(ctx, flowID, rpc, mapToRPCInput(event), &output)
		if err == nil {
			log.Debug(ctx, "trigger event delivered", slog.String("event_id", event.ID), slog.String("flow_id", flowID))
			return nil
		}
		var mappingFailure *dex.ValueMappingError
		if errors.As(err, &mappingFailure) && mappingFailure.Operation == "decode" {
			log.Warn(ctx, "trigger event delivered; rpc response undecodable",
				slog.String("event_id", event.ID), slog.String("flow_id", flowID), triggerlog.Err(err))
			return nil
		}
		return classifyDexTriggerError(err)
	})
}
