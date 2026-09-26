// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/internal/triggerlog"
	"github.com/superdurable/dex/sdk-go/dex"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	triggerRetryInitialDelay = 250 * time.Millisecond
	triggerRetryMaxDelay     = 30 * time.Second
)

// undeliverableTriggerMessage starts every UndeliverableTriggerError message. A Dex Worker reports the
// handler's error message as its detail, so the RPC target recognizes a handler's marker by this prefix.
const undeliverableTriggerMessage = "Trigger event is undeliverable"

// UndeliverableTriggerError reports that no retry can deliver a Trigger event to its target.
// Durable Trigger inboxes, Trigger runners, and DeliverTrigger consume the event instead of retrying it.
// Its gRPC status is FailedPrecondition, so a Dex RPC handler can return it to reject a Trigger event.
// The handler must return the marker itself, not an error that wraps it, so the Worker reports the
// marker's message first.
type UndeliverableTriggerError struct {
	// Err explains why the event cannot be delivered.
	Err error
}

// Error describes the undeliverable event.
// It returns "<nil>" for a nil receiver, "Trigger event is undeliverable" when Err is nil,
// and otherwise "Trigger event is undeliverable: " followed by Err.Error().
func (e *UndeliverableTriggerError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Err == nil {
		return undeliverableTriggerMessage
	}
	return undeliverableTriggerMessage + ": " + e.Err.Error()
}

// Unwrap returns the reason the event cannot be delivered. It returns nil for a nil receiver.
func (e *UndeliverableTriggerError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// GRPCStatus reports FailedPrecondition so a Dex Worker preserves the classification across an RPC.
func (e *UndeliverableTriggerError) GRPCStatus() *status.Status {
	return status.New(codes.FailedPrecondition, e.Error())
}

// MarkTriggerUndeliverable wraps err so Trigger delivery consumes the event instead of retrying it.
// It returns nil when err is nil and returns err unchanged when err is itself an
// *UndeliverableTriggerError. It wraps any other error, including one that wraps a marker, so the result
// is always the outermost error an RPC handler returns.
func MarkTriggerUndeliverable(err error) error {
	if err == nil {
		return nil
	}
	if marked, ok := err.(*UndeliverableTriggerError); ok {
		return marked
	}
	return &UndeliverableTriggerError{Err: err}
}

// IsTriggerUndeliverable reports whether err or an error it wraps is an UndeliverableTriggerError.
func IsTriggerUndeliverable(err error) bool {
	var undeliverable *UndeliverableTriggerError
	return errors.As(err, &undeliverable)
}

// TriggerOption configures optional behavior of DeliverTrigger and the Dex Trigger targets.
type TriggerOption interface {
	applyTriggerOption(*triggerOptions)
}

type triggerOptions struct {
	logger *slog.Logger
}

type triggerLoggerOption struct{ logger *slog.Logger }

func (option triggerLoggerOption) applyTriggerOption(options *triggerOptions) {
	options.logger = option.logger
}

// WithTriggerLogger sends Trigger delivery records to logger. Without this option, or with a nil logger,
// records go to slog.Default() as of each record. Records carry IDs and error messages, never event
// payloads. Add identifying attributes, such as a binding name, with logger.With.
func WithTriggerLogger(logger *slog.Logger) TriggerOption {
	return triggerLoggerOption{logger: logger}
}

func resolveTriggerOptions(options []TriggerOption) triggerOptions {
	var resolved triggerOptions
	for _, option := range options {
		if option != nil {
			option.applyTriggerOption(&resolved)
		}
	}
	return resolved
}

// DeliverTrigger hands one acknowledged event to target until target consumes it.
// A nil result or an UndeliverableTriggerError consumes the event. Every other error is retried after a
// capped exponential delay that starts at 250 milliseconds and doubles up to 30 seconds.
// DeliverTrigger returns nil once the event is consumed, or ctx.Err() as soon as ctx ends, including
// while it waits between attempts.
//
// It logs a WARN "trigger delivery failed; retrying" record with the attempt number and the next delay
// before every wait, an INFO "trigger delivered after retry" record when a later attempt succeeds, and a
// WARN "trigger event skipped: undeliverable" record when target returns an UndeliverableTriggerError.
// A target that consumes an undeliverable event itself, such as a durable inbox, logs that skip instead.
// Pass WithTriggerLogger to choose the logger. Every attempt runs through TriggerAttempt, and the
// records carry the attributes of ContextWithTriggerLogAttrs.
func DeliverTrigger[T any](ctx context.Context, target TriggerTarget[T], event TriggerEvent[T], options ...TriggerOption) error {
	log := triggerlog.New(resolveTriggerOptions(options).logger)
	eventID := slog.String("event_id", event.ID)
	for attempt := 1; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		attemptCtx, skippedInside := TriggerAttempt(ctx)
		err := target.HandleTrigger(attemptCtx, event)
		if err == nil {
			if attempt > 1 && !skippedInside() {
				log.Info(ctx, "trigger delivered after retry", eventID, slog.Int("attempts", attempt))
			}
			return nil
		}
		if IsTriggerUndeliverable(err) {
			log.Warn(ctx, "trigger event skipped: undeliverable", append([]slog.Attr{eventID}, triggerlog.ErrAttrs(err)...)...)
			return nil
		}
		if err := ctx.Err(); err != nil {
			// The attempt failed because the context ended, so there is no retry to report.
			return err
		}
		delay := triggerRetryDelay(attempt)
		log.Warn(ctx, "trigger delivery failed; retrying", append(
			[]slog.Attr{eventID, slog.Int("attempt", attempt), slog.Duration("delay", delay)}, triggerlog.ErrAttrs(err)...,
		)...)
		if err := waitForTriggerRetry(ctx, delay); err != nil {
			return err
		}
	}
}

// TriggerAttempt starts one delivery attempt for a source that retries on its own schedule, such as a
// poller, instead of with DeliverTrigger. Pass the returned context to the target's HandleTrigger. When
// HandleTrigger returns nil, skipped reports whether a component inside the attempt, such as a durable
// inbox or a NewTrigger runner, consumed the event as undeliverable and already logged that skip. Log
// "trigger delivered after retry" only when skipped returns false, so every skip appears once.
func TriggerAttempt(ctx context.Context) (attemptCtx context.Context, skipped func() bool) {
	return triggerlog.WithSkipRecorder(ctx)
}

// ContextWithTriggerLogAttrs returns a context whose Trigger delivery records also carry attrs:
// DeliverTrigger's, and those of the durable inbox, the Dex targets, and NewTrigger runners inside it.
// Use it for event-scoped IDs, such as a channel or thread ID, that the target's own logger lacks, so
// every record about one event can be found by the same attribute. A record keeps its own value for a
// key it already carries. Never pass message text, payloads, or credentials.
func ContextWithTriggerLogAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	return triggerlog.ContextWithAttrs(ctx, attrs...)
}

// waitForTriggerRetry waits for delay and returns ctx.Err() as soon as ctx ends. Tests replace it to
// observe the retry schedule.
var waitForTriggerRetry = func(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// triggerRetryDelay returns the wait after failed attempt number attempt, starting at 1.
func triggerRetryDelay(attempt int) time.Duration {
	delay := triggerRetryInitialDelay
	for completed := 1; completed < attempt && delay < triggerRetryMaxDelay; completed++ {
		delay *= 2
	}
	return min(delay, triggerRetryMaxDelay)
}

// classifyDexTriggerError marks the Dex errors that no retry of the same event can fix: a closed or
// never-started Flow, an RPC handler's own UndeliverableTriggerError, and an input that cannot be
// encoded. Every other error stays retryable, including a request that the Dex Server rejects as
// invalid, because that depends on the Flow definition and the server configuration rather than on the
// event. Typed Dex errors embed ServiceError, so each type is checked explicitly.
func classifyDexTriggerError(err error) error {
	if err == nil || IsTriggerUndeliverable(err) {
		return err
	}
	var notActive *dex.FlowNotActiveError
	var notFound *dex.FlowNotFoundError
	if errors.As(err, &notActive) || errors.As(err, &notFound) {
		return MarkTriggerUndeliverable(err)
	}
	var workerFailure *dex.WorkerInvocationError
	if errors.As(err, &workerFailure) {
		if isHandlerUndeliverableMarker(workerFailure.Worker) {
			return MarkTriggerUndeliverable(err)
		}
		return err
	}
	var mappingFailure *dex.ValueMappingError
	if errors.As(err, &mappingFailure) && mappingFailure.Operation == "encode" {
		return MarkTriggerUndeliverable(err)
	}
	return err
}

// isHandlerUndeliverableMarker reports whether the Worker failure is an RPC handler's own
// UndeliverableTriggerError. Dex reports every Worker failure with an outer FailedPrecondition code,
// and a handler that returns another Flow's Dex error unchanged also has a FailedPrecondition Worker
// code, so the marker's message prefix is required as well.
func isHandlerUndeliverableMarker(worker *dex.WorkerError) bool {
	return worker != nil && worker.Code == codes.FailedPrecondition &&
		strings.HasPrefix(worker.Detail, undeliverableTriggerMessage)
}

// undeliverableConsumingTarget is the target a TriggerRunner passes to its source. It logs and consumes
// the undeliverable events its target returns.
type undeliverableConsumingTarget[T any] struct {
	target TriggerTarget[T]
	log    triggerlog.Logger
}

func (target undeliverableConsumingTarget[T]) HandleTrigger(ctx context.Context, event TriggerEvent[T]) error {
	err := target.target.HandleTrigger(ctx, event)
	if IsTriggerUndeliverable(err) {
		target.log.Warn(ctx, "trigger event skipped: undeliverable",
			append([]slog.Attr{slog.String("event_id", event.ID)}, triggerlog.ErrAttrs(err)...)...)
		triggerlog.RecordSkip(ctx)
		return nil
	}
	return err
}

func (target undeliverableConsumingTarget[T]) PrepareTrigger(ctx context.Context, event TriggerEvent[T]) error {
	return PrepareTriggerDelivery(ctx, target.target, event)
}

// triggerBindingAttrs identifies a Trigger binding in log records.
func triggerBindingAttrs(binding TriggerBindingRef) []slog.Attr {
	return []slog.Attr{
		slog.String("connector", binding.Trigger.ConnectorID), slog.String("connection", binding.Connection.Name),
		slog.String("trigger", binding.Trigger.TriggerName), slog.String("binding", binding.Name),
	}
}
