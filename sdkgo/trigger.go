// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo/internal/triggerlog"
	"github.com/superdurable/dex/sdk-go/dex"
	"google.golang.org/grpc/codes"
)

// TriggerRef is the stable manifest identity of one connector trigger.
type TriggerRef struct {
	// ConnectorID identifies the connector manifest.
	ConnectorID string `json:"connectorId" yaml:"connectorId"`
	// TriggerName identifies the Trigger within its connector manifest.
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
	// Trigger is the stable manifest identity for the provider event source.
	Trigger TriggerRef `json:"trigger" yaml:"trigger"`
	// Description explains the provider event source to application tooling.
	Description string `json:"description" yaml:"description"`
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
	// Connection selects the credential reference used by the source.
	Connection ConnectionRef `json:"connection" yaml:"connection"`
	// Trigger identifies the provider event source.
	Trigger TriggerRef `json:"trigger" yaml:"trigger"`
	// Name is the stable application binding name.
	Name string `json:"name" yaml:"name"`
}

// TriggerBindingDefinition exposes one static application binding to tooling.
type TriggerBindingDefinition struct {
	// Definition is the immutable Trigger definition.
	Definition TriggerDefinition `json:"definition" yaml:"definition"`
	// ConnectionName names the configured provider connection.
	ConnectionName string `json:"connectionName" yaml:"connectionName"`
	// BindingName names this configured Trigger binding.
	BindingName string `json:"bindingName" yaml:"bindingName"`
	// ConfigurationUI describes optional Connector Studio configuration units.
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
	// ID is the stable provider identifier.
	ID string `json:"eventId"`
	// OccurredAt records when the provider event occurred.
	OccurredAt time.Time `json:"occurredAt"`
	// Payload is the connector-normalized provider event body.
	Payload T `json:"payload"`
}

// TriggerTarget handles one event after the provider transport acknowledges receipt.
// HandleTrigger returns nil when it consumes the event, an UndeliverableTriggerError when no retry can
// deliver it, or another error to keep the event pending for a retry.
type TriggerTarget[T any] interface {
	// HandleTrigger consumes, retries, or marks one provider event undeliverable.
	HandleTrigger(context.Context, TriggerEvent[T]) error
}

// TriggerDeliveryPreparer persists an event before a provider acknowledges it.
// A prepared event stays pending until the target consumes it by returning nil or an
// UndeliverableTriggerError.
type TriggerDeliveryPreparer[T any] interface {
	// PrepareTrigger persists an event before provider acknowledgement.
	PrepareTrigger(context.Context, TriggerEvent[T]) error
}

// TriggerDeliveryReplayer redelivers events persisted before a previous process stopped.
// ReplayTriggerDeliveries retries target failures instead of returning them. It returns nil after every
// event pending at the call has been consumed, ctx.Err() when ctx ends first, or an error when the
// persisted events cannot be read.
type TriggerDeliveryReplayer interface {
	// ReplayTriggerDeliveries drains events left pending by an earlier process.
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
	// Run processes provider events until cancellation or an unrecoverable error.
	Run(context.Context, TriggerTarget[T]) error
}

// TriggerRunner is a configured long-running connector trigger.
type TriggerRunner interface {
	// Run processes provider events until cancellation or an unrecoverable error.
	Run(context.Context) error
	// Definition returns the immutable connector operation definition.
	Definition() TriggerDefinition
	// Binding returns the configured Trigger binding identity.
	Binding() TriggerBindingRef
}

// TriggerConfig configures one provider source and application target.
type TriggerConfig[T any] struct {
	// Definition is the immutable Trigger definition.
	Definition TriggerDefinition
	// Binding is the configured Trigger binding identity.
	Binding TriggerBindingRef
	// Source polls or subscribes to provider events.
	Source TriggerSource[T]
	// Target routes accepted events into the application.
	Target TriggerTarget[T]
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

// Run processes provider events until cancellation or an unrecoverable error.
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

// Definition returns the immutable connector operation definition.
func (runner *triggerRunner[T]) Definition() TriggerDefinition { return runner.definition }

// Binding returns the configured Trigger binding identity.
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
// The event ID is the Flow-start request ID and Flow ID reuse is disallowed, so a duplicate start returns
// nil even after the original Flow completes. An empty Flow ID or event ID, or an input that cannot be
// encoded, returns an UndeliverableTriggerError. Every other error is
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
		_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{
			IDReusePolicy: dex.IDReuseDisallow,
			RequestID:     &requestID,
		})
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
//
// Every delivery attempt of one event invokes the RPC with the same request ID, TriggerRPCRequestID of the
// RPC name and the event ID, through dex.Client.InvokeRPCWithOptions, so Dex Server applies the RPC's Flow
// changes at most once per event while the Flow run keeps that request ID: after a durable inbox replay, a
// provider or relay retry, or a retry after a timeout that hid an applied RPC. The RPC name is the one
// given with WithTriggerRPCName, or else the RPC's Go method name, so the IDs match the TypeScript SDK's
// newDexRPCTriggerTarget for the same RPC name. Set WithTriggerRPCName to the RPC's wire name when the RPC
// registers a dex.RPCOptions.Name. Without it, renaming the method changes the IDs of events still being
// redelivered. Without WithTriggerRPCName, an rpc whose method name cannot be read, such as a value that
// is not a bound method, is invoked without a request ID.
//
// A Flow run keeps only its newest request IDs, as many as the Dex Server limit
// limits.maxRequestIdsPerRun (1000 by default), shared with its other requests that carry request IDs,
// such as Client Attribute writes; a redelivery after its ID was evicted applies again. Register the RPC
// with dex.RPCOptions.IsTransactional or LockAttributes, so Dex answers a repeat with the recorded reply
// without running the handler again. Any other handler runs again on a repeat, and Dex discards that
// attempt's Flow changes when it makes any. Dex answers a repeat this way also after the Flow closed, until
// it deletes the closed run, so the target consumes such a redelivery as delivered. A handler that must
// also ignore repeats beyond the request-ID window keeps its own record of event IDs. The ID covers the RPC name
// and the event ID only, never the mapped input or the binding, so two bindings that route one event to
// the same RPC on the same Flow share one ID and Dex applies only the first; give each such binding its
// own RPC.
//
// Keep mapToRPCInput deterministic. Dex Server 0.1.37 and later answer a repeat whose request differs, such
// as one whose mapper returned another input, with the first request's reply and apply none of its
// changes. Earlier servers reject it with INVALID_ARGUMENT "request ID ... was already used for a different
// request", also while the earlier request is still in flight. The target then logs an ERROR "trigger rpc
// request conflict; event dropped" record and returns a TriggerRPCRequestConflictError wrapped in an
// UndeliverableTriggerError, so delivery drops the event; when that earlier request later fails without
// committing, the event is lost. Dex payload encryption (dex.ClientOptions.PayloadEncryption) encrypts each
// attempt's input with a fresh nonce, so to those earlier servers every repeat is a different request, even
// from a deterministic mapper: every redelivery of an applied event is dropped as a conflict, and a retry
// while an earlier attempt is still in flight can lose the event. Use Dex Server 0.1.37 or later with payload
// encryption.
//
// It returns an UndeliverableTriggerError when the Flow was never started, when the Flow has closed and
// does not hold the event's request ID, when the RPC handler returns the error from
// MarkTriggerUndeliverable as its outermost error, when the input cannot be encoded, when the Flow ID or
// event ID is empty, or on a request ID conflict. The target cannot tell an event that arrived before its
// Flow started from one whose Flow never existed, so deliver a Flow's start before its RPC events. A
// response that cannot be decoded after Dex applied the RPC returns nil because the target discards the
// output. Every other error is returned unchanged for a retry, including other handler errors and a
// handler that returns another Flow's Dex error.
// It logs an INFO "trigger event skipped: filtered" record when the filter rejects an event, a DEBUG
// "trigger event delivered" record when Dex applies the RPC, and a WARN record when Dex applied the RPC
// but its response cannot be decoded; pass WithTriggerLogger to choose the logger. The delivered, WARN,
// and conflict records carry the request_id the target sent. The caller that consumes or retries a
// returned error logs it.
//
// It panics when a required argument is nil, or when WithTriggerRPCName names a blank name or one that
// contains a NUL character.
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
	return newDexRPCTriggerTarget[EVENT](client, rpc, filterEvent, resolveFlowID, mapToRPCInput, options...)
}

// TriggerRPCRequestConflictError reports that Dex Server rejected an RPC Trigger delivery because an
// earlier attempt of the same event sent a different request under the same request ID.
//
// Dex Server 0.1.36 and earlier compare the whole request, so the cause is a non-deterministic or changed
// input mapper, another binding that routes the same event to the same RPC on the same Flow, changed RPC
// options such as the timeout or locks, or Dex payload encryption, which encrypts each attempt's input with
// a fresh nonce and so makes every repeat a different request. Those servers report the same conflict
// while the earlier request is still in flight. NewDexRPCTriggerTarget returns it wrapped in an
// UndeliverableTriggerError, so delivery drops the event; match it with errors.As. Dex Server 0.1.37 and
// later compare only the request ID and answer such a repeat with the first request's reply instead.
type TriggerRPCRequestConflictError struct {
	// EventID is the Trigger event ID.
	EventID string
	// FlowID is the resolved Flow ID.
	FlowID string
	// RPCName is the RPC name in the request ID: the WithTriggerRPCName value or the RPC's Go method name.
	RPCName string
	// RequestID is the request ID this attempt sent, from TriggerRPCRequestID.
	RequestID string
	// Err is the *dex.ServiceError that reported the conflict.
	Err error
}

// Error describes the conflict and its possible causes without the request input. Wrapped in an
// UndeliverableTriggerError, the message is the TypeScript SDK's TriggerRPCRequestConflictError message
// with one more cause: Dex payload encryption on a Dex Server before 0.1.37.
func (e *TriggerRPCRequestConflictError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("RPC trigger event %s for RPC %s on Flow %s mapped to a different RPC request than an earlier attempt "+
		"with request ID %s (non-deterministic or changed input mapper, another binding, changed RPC options, "+
		"or Dex payload encryption on a Dex Server before 0.1.37); the event was dropped",
		e.EventID, e.RPCName, e.FlowID, e.RequestID)
}

// Unwrap returns the Dex error that reported the conflict. It returns nil for a nil receiver.
func (e *TriggerRPCRequestConflictError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// TriggerRPCRequestIDPrefix starts every request ID that NewDexRPCTriggerTarget derives.
const TriggerRPCRequestIDPrefix = "trigger-rpc-"

// TriggerRPCRequestID derives the Dex request ID that NewDexRPCTriggerTarget sends on every delivery
// attempt of one event. It matches the TypeScript SDK's triggerRPCRequestId byte for byte.
//
// The ID is TriggerRPCRequestIDPrefix followed by the first 16 bytes, as unpadded base64url (22
// characters), of the SHA-256 digest of the UTF-8 bytes of rpcName, one 0x00 byte, and the UTF-8 bytes of
// eventID. Each byte of rpcName or eventID that is not part of a valid UTF-8 sequence counts as U+FFFD, as
// encoding/json and the durable Trigger inbox store it, and as the TypeScript SDK encodes an unpaired
// surrogate. For example, rpcName "receiveReply" and eventID "Ev1" hash the 16 bytes "receiveReply\x00Ev1"
// and yield "trigger-rpc-kDFFz9I4ELDtzXkmvRTzlA". It returns an error when rpcName is empty or contains a
// NUL character.
func TriggerRPCRequestID(rpcName string, eventID string) (string, error) {
	if rpcName == "" || strings.ContainsRune(rpcName, 0) {
		return "", errors.New("RPC request ID requires an RPC name without NUL characters")
	}
	return deriveTriggerRPCRequestID(rpcName, eventID), nil
}

// dexRPCInvoker is the dex.Client method the RPC target calls; unit tests record its requests.
type dexRPCInvoker interface {
	InvokeRPCWithOptions(ctx context.Context, flowID string, rpc any, input any, outputPtr any, options dex.RPCInvokeOptions) error
}

// dexRPCTriggerTarget is the target NewDexRPCTriggerTarget returns.
type dexRPCTriggerTarget[EVENT, INPUT, OUTPUT any] struct {
	client        dexRPCInvoker
	rpc           dex.RPC[INPUT, OUTPUT]
	filterEvent   TriggerFilter[EVENT]
	resolveFlowID FlowIDResolver[EVENT]
	mapToRPCInput RPCInputMapper[EVENT, INPUT]
	// rpcName is empty when the target sends no request ID.
	rpcName string
	log     triggerlog.Logger
}

func newDexRPCTriggerTarget[EVENT, INPUT, OUTPUT any](
	client dexRPCInvoker,
	rpc dex.RPC[INPUT, OUTPUT],
	filterEvent TriggerFilter[EVENT],
	resolveFlowID FlowIDResolver[EVENT],
	mapToRPCInput RPCInputMapper[EVENT, INPUT],
	options ...TriggerOption,
) *dexRPCTriggerTarget[EVENT, INPUT, OUTPUT] {
	resolved := resolveTriggerOptions(options)
	rpcName := resolved.rpcName
	if resolved.hasRPCName && !isTriggerRPCNameUsable(rpcName) {
		panic("RPC trigger RPC name must contain a non-blank character and no NUL characters")
	}
	if !resolved.hasRPCName {
		rpcName = triggerRPCMethodName(rpc)
	}
	return &dexRPCTriggerTarget[EVENT, INPUT, OUTPUT]{
		client: client, rpc: rpc, filterEvent: filterEvent, resolveFlowID: resolveFlowID, mapToRPCInput: mapToRPCInput,
		rpcName: rpcName, log: triggerlog.New(resolved.logger, slog.String("target", "rpc")),
	}
}

// HandleTrigger invokes the RPC for one accepted event and classifies the Dex result.
func (target *dexRPCTriggerTarget[EVENT, INPUT, OUTPUT]) HandleTrigger(ctx context.Context, event TriggerEvent[EVENT]) error {
	if !target.filterEvent(event) {
		target.log.Info(ctx, "trigger event skipped: filtered", slog.String("event_id", event.ID))
		return nil
	}
	flowID := target.resolveFlowID(event)
	if strings.TrimSpace(flowID) == "" || strings.TrimSpace(event.ID) == "" {
		return MarkTriggerUndeliverable(fmt.Errorf("RPC trigger requires Flow ID and event ID"))
	}
	var invokeOptions dex.RPCInvokeOptions
	var requestAttrs []slog.Attr
	requestID := ""
	if target.rpcName != "" {
		requestID = deriveTriggerRPCRequestID(target.rpcName, event.ID)
		invokeOptions.RequestID = &requestID
		requestAttrs = []slog.Attr{slog.String("request_id", requestID)}
	}
	var output OUTPUT
	err := target.client.InvokeRPCWithOptions(ctx, flowID, target.rpc, target.mapToRPCInput(event), &output, invokeOptions)
	if err == nil {
		target.log.Debug(ctx, "trigger event delivered",
			append([]slog.Attr{slog.String("event_id", event.ID), slog.String("flow_id", flowID)}, requestAttrs...)...)
		return nil
	}
	var mappingFailure *dex.ValueMappingError
	if errors.As(err, &mappingFailure) && mappingFailure.Operation == "decode" {
		target.log.Warn(ctx, "trigger event delivered; rpc response undecodable",
			append(append([]slog.Attr{slog.String("event_id", event.ID), slog.String("flow_id", flowID)}, requestAttrs...), triggerlog.Err(err))...)
		return nil
	}
	if requestID != "" && isRequestIDUsedForDifferentRequest(err, requestID) {
		target.log.Error(ctx, "trigger rpc request conflict; event dropped",
			slog.String("event_id", event.ID), slog.String("request_id", requestID),
			slog.String("rpc_name", target.rpcName), slog.String("flow_id", flowID))
		return MarkTriggerUndeliverable(&TriggerRPCRequestConflictError{
			EventID: event.ID, FlowID: flowID, RPCName: target.rpcName, RequestID: requestID, Err: err,
		})
	}
	return classifyDexTriggerError(err)
}

// isRequestIDUsedForDifferentRequest reports a conflict only when the detail names this call's own
// request ID.
func isRequestIDUsedForDifferentRequest(err error, requestID string) bool {
	var serviceError *dex.ServiceError
	return errors.As(err, &serviceError) && serviceError.Code == codes.InvalidArgument &&
		strings.Contains(serviceError.Detail, "request ID "+requestID+" was already used for a different request")
}

// triggerRPCMethodName returns a bound method's Go name, as the Dex Client resolves the RPC, or "" otherwise.
func triggerRPCMethodName(rpc any) string {
	value := reflect.ValueOf(rpc)
	if value.Kind() != reflect.Func || value.IsNil() {
		return ""
	}
	function := runtime.FuncForPC(value.Pointer())
	if function == nil {
		return ""
	}
	qualifiedName, isBoundMethod := strings.CutSuffix(function.Name(), "-fm")
	separator := strings.LastIndex(qualifiedName, ".")
	if !isBoundMethod || separator < 0 {
		return ""
	}
	methodName := qualifiedName[separator+1:]
	if !isTriggerRPCNameUsable(methodName) {
		return ""
	}
	return methodName
}

// isTriggerRPCNameUsable reports whether a name can derive a request ID: it has a non-blank character and
// no NUL.
func isTriggerRPCNameUsable(rpcName string) bool {
	return strings.TrimSpace(rpcName) != "" && !strings.ContainsRune(rpcName, 0)
}

// deriveTriggerRPCRequestID derives TriggerRPCRequestID for an RPC name already checked to be usable.
func deriveTriggerRPCRequestID(rpcName string, eventID string) string {
	digest := sha256.Sum256([]byte(replaceInvalidUTF8Bytes(rpcName) + "\x00" + replaceInvalidUTF8Bytes(eventID)))
	return TriggerRPCRequestIDPrefix + base64.RawURLEncoding.EncodeToString(digest[:16])
}

// replaceInvalidUTF8Bytes maps each invalid byte to U+FFFD, as encoding/json does, so inbox replays keep
// their request ID.
func replaceInvalidUTF8Bytes(text string) string {
	if utf8.ValidString(text) {
		return text
	}
	return string([]rune(text))
}
