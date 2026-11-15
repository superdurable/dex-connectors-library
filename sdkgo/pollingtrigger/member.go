// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pollingtrigger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/triggerlog"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// FeedEventSelector maps one provider change to zero or more of a member's Trigger events. previous is
// the change's entry in the feed snapshot committed before this page, or nil when the change type does
// not implement KeyedItem, the feed keeps no snapshot, or the key is untracked. A selector must be
// deterministic and side-effect free: a page is selected again when it is refetched after a failure.
type FeedEventSelector[CHANGE, EVENT any] func(change CHANGE, previous *projectconfig.KeyedSnapshotEntry) []sdkgo.TriggerEvent[EVENT]

// FeedMember is one Trigger binding's view of a shared provider change feed. It is sealed: build it with
// NewFeedMember.
type FeedMember[CHANGE any] interface {
	memberBinding() sdkgo.TriggerBindingRef
	pollMember() pollMember[CHANGE]
}

// pollMember is the type-erased delivery state of one binding of a poller.
type pollMember[CHANGE any] interface {
	memberBinding() sdkgo.TriggerBindingRef
	attachLogger(logger *slog.Logger)
	replay(ctx context.Context) error
	stage(ctx context.Context, changes []CHANGE, previous func(CHANGE) *projectconfig.KeyedSnapshotEntry, revision string)
	recordStaged(ctx context.Context, turn *pollTurn) error
	acceptStaged()
	stagedCount() int
	drain(ctx context.Context, turn *pollTurn) error
}

// memberDeliveryMode says when a member's events are handed to its target relative to the cursor commit.
type memberDeliveryMode int

const (
	// recordBeforeCommit records events in a durable target, commits, then delivers.
	recordBeforeCommit memberDeliveryMode = iota
	// deliverBeforeCommit consumes events in a non-durable target before the commit.
	deliverBeforeCommit
	// recordAndDeliverBeforeCommit prepares and consumes each event before the commit, for a target
	// whose durability is unknown, such as the one sdkgo.NewTrigger passes to a TriggerSource.
	recordAndDeliverBeforeCommit
)

type feedMember[CHANGE, EVENT any] struct {
	definition   sdkgo.TriggerDefinition
	binding      sdkgo.TriggerBindingRef
	selectEvents FeedEventSelector[CHANGE, EVENT]
	target       sdkgo.TriggerTarget[EVENT]
	mode         memberDeliveryMode
	shouldReplay bool
	log          triggerlog.Logger
	// staged are this member's events selected from the page being processed.
	staged []sdkgo.TriggerEvent[EVENT]
	// processed are the events recorded or consumed at processedRevision; a retry skips their IDs even
	// when the provider now returns a different page.
	processedRevision string
	processedIDs      map[string]bool
	processed         []sdkgo.TriggerEvent[EVENT]
	// queue holds recorded events whose cursor is committed but that the target has not consumed.
	queue         []sdkgo.TriggerEvent[EVENT]
	retryEventID  string
	retryFailures int
}

// NewFeedMember binds one Trigger binding to a feed with its selector and target. A target that
// implements sdkgo.TriggerDeliveryPreparer, such as provider.NewDurableTriggerTarget, records events
// before the feed cursor commits and receives them after; any other target consumes them before the
// commit. A target that implements sdkgo.TriggerDeliveryReplayer is replayed when the runner starts.
func NewFeedMember[CHANGE, EVENT any](
	definition sdkgo.TriggerDefinition,
	binding sdkgo.TriggerBindingRef,
	selectEvents FeedEventSelector[CHANGE, EVENT],
	target sdkgo.TriggerTarget[EVENT],
) (FeedMember[CHANGE], error) {
	if err := validateMemberBinding(definition, binding); err != nil {
		return nil, err
	}
	if selectEvents == nil || target == nil {
		return nil, errors.New("poll feed member selector and target are required")
	}
	return newFeedMember(definition, binding, selectEvents, target, deliveryModeFor(target), true), nil
}

func newFeedMember[CHANGE, EVENT any](
	definition sdkgo.TriggerDefinition,
	binding sdkgo.TriggerBindingRef,
	selectEvents FeedEventSelector[CHANGE, EVENT],
	target sdkgo.TriggerTarget[EVENT],
	mode memberDeliveryMode,
	shouldReplay bool,
) *feedMember[CHANGE, EVENT] {
	return &feedMember[CHANGE, EVENT]{
		definition: definition, binding: binding, selectEvents: selectEvents, target: target, mode: mode,
		shouldReplay: shouldReplay, log: triggerlog.New(nil, bindingAttrs(binding)...),
	}
}

func deliveryModeFor[EVENT any](target sdkgo.TriggerTarget[EVENT]) memberDeliveryMode {
	if _, isDurable := target.(sdkgo.TriggerDeliveryPreparer[EVENT]); isDurable {
		return recordBeforeCommit
	}
	return deliverBeforeCommit
}

func validateMemberBinding(definition sdkgo.TriggerDefinition, binding sdkgo.TriggerBindingRef) error {
	if err := definition.Validate(); err != nil {
		return fmt.Errorf("poll trigger definition: %w", err)
	}
	if err := binding.Validate(); err != nil {
		return fmt.Errorf("poll trigger binding: %w", err)
	}
	if binding.Trigger != definition.Trigger {
		return errors.New("poll trigger binding does not match its definition")
	}
	return nil
}

func (member *feedMember[CHANGE, EVENT]) memberBinding() sdkgo.TriggerBindingRef {
	return member.binding
}

func (member *feedMember[CHANGE, EVENT]) pollMember() pollMember[CHANGE] { return member }

func (member *feedMember[CHANGE, EVENT]) attachLogger(logger *slog.Logger) {
	member.log = triggerlog.New(logger, bindingAttrs(member.binding)...)
}

func (member *feedMember[CHANGE, EVENT]) replay(ctx context.Context) error {
	if !member.shouldReplay {
		return nil
	}
	if replayer, ok := member.target.(sdkgo.TriggerDeliveryReplayer); ok {
		if err := replayer.ReplayTriggerDeliveries(ctx); err != nil {
			return fmt.Errorf("replay Trigger deliveries of binding %q: %w", member.binding.Name, err)
		}
	}
	return nil
}

// stage selects this member's events from a page fetched at the cursor revision.
func (member *feedMember[CHANGE, EVENT]) stage(
	ctx context.Context,
	changes []CHANGE,
	previous func(CHANGE) *projectconfig.KeyedSnapshotEntry,
	revision string,
) {
	member.staged = member.staged[:0]
	for _, change := range changes {
		for _, event := range member.selectEvents(change, previous(change)) {
			if strings.TrimSpace(event.ID) == "" {
				member.log.Warn(ctx, "trigger event skipped: undeliverable",
					slog.String("event_id", event.ID), slog.String("error", "Trigger event ID is required"))
				continue
			}
			member.staged = append(member.staged, event)
		}
	}
	if member.processedRevision != revision {
		// The cursor moved without this poller's commit, such as after an unknown write outcome.
		member.settleProcessed()
		member.processedRevision = revision
	}
}

// recordStaged records or consumes the staged events not yet processed at this revision, in order, and
// stops at the first failure so the cursor does not commit.
func (member *feedMember[CHANGE, EVENT]) recordStaged(ctx context.Context, turn *pollTurn) error {
	for _, event := range member.staged {
		if member.processedIDs[event.ID] {
			continue
		}
		if member.mode != deliverBeforeCommit {
			if err := sdkgo.PrepareTriggerDelivery(ctx, member.target, event); err != nil {
				return &recordFailedError{err: err}
			}
		}
		if member.mode != recordBeforeCommit {
			if err := member.deliver(ctx, turn, event); err != nil {
				return err
			}
		}
		if member.processedIDs == nil {
			member.processedIDs = map[string]bool{}
		}
		member.processedIDs[event.ID] = true
		member.processed = append(member.processed, event)
	}
	return nil
}

// acceptStaged moves recorded events to the delivery queue once the cursor moved past them, by this
// poller's commit or a concurrent replica's.
func (member *feedMember[CHANGE, EVENT]) acceptStaged() {
	member.settleProcessed()
	member.staged = member.staged[:0]
	member.processedRevision = ""
}

// settleProcessed queues the events recorded at the processed revision, even ones a refetch dropped.
func (member *feedMember[CHANGE, EVENT]) settleProcessed() {
	if member.mode == recordBeforeCommit {
		member.queue = append(member.queue, member.processed...)
	}
	member.processed, member.processedIDs = nil, nil
}

func (member *feedMember[CHANGE, EVENT]) stagedCount() int { return len(member.staged) }

// drain delivers queued events in order and stops at the first retryable failure.
func (member *feedMember[CHANGE, EVENT]) drain(ctx context.Context, turn *pollTurn) error {
	for len(member.queue) > 0 {
		if err := member.deliver(ctx, turn, member.queue[0]); err != nil {
			return err
		}
		member.queue = member.queue[1:]
	}
	member.queue = nil
	return nil
}

// deliver makes one delivery attempt with the same records and skip accounting as sdkgo.DeliverTrigger.
// A failure is retried on a later poll, before any later event of this member.
func (member *feedMember[CHANGE, EVENT]) deliver(ctx context.Context, turn *pollTurn, event sdkgo.TriggerEvent[EVENT]) error {
	failures := 0
	if member.retryEventID == event.ID {
		failures = member.retryFailures
	}
	eventID := slog.String("event_id", event.ID)
	attemptCtx, skippedInside := sdkgo.TriggerAttempt(ctx)
	err := member.target.HandleTrigger(attemptCtx, event)
	switch {
	case err == nil:
		if failures > 0 && !skippedInside() {
			member.log.Info(ctx, "trigger delivered after retry", eventID, slog.Int("attempts", failures+1))
		}
	case sdkgo.IsTriggerUndeliverable(err):
		member.log.Warn(ctx, "trigger event skipped: undeliverable", append([]slog.Attr{eventID}, triggerlog.ErrAttrs(err)...)...)
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		member.retryEventID, member.retryFailures = event.ID, failures+1
		member.log.Warn(ctx, "trigger delivery failed; retrying", append([]slog.Attr{
			eventID, slog.Int("attempt", failures+1), slog.Duration("delay", turn.failureDelay()),
		}, triggerlog.ErrAttrs(err)...)...)
		return &deliveryFailedError{err: err}
	}
	member.retryEventID, member.retryFailures = "", 0
	return nil
}

// recordFailedError is a failure to record an event in a durable target before the commit.
type recordFailedError struct{ err error }

// Error describes the failure.
func (e *recordFailedError) Error() string { return "record Trigger event: " + e.err.Error() }

// Unwrap returns the underlying failure.
func (e *recordFailedError) Unwrap() error { return e.err }

// deliveryFailedError is a target failure that deliver already logged.
type deliveryFailedError struct{ err error }

// Error describes the failure.
func (e *deliveryFailedError) Error() string { return "deliver Trigger event: " + e.err.Error() }

// Unwrap returns the underlying failure.
func (e *deliveryFailedError) Unwrap() error { return e.err }

// bindingAttrs identifies a Trigger binding in log records, like the sdkgo Trigger runners.
func bindingAttrs(binding sdkgo.TriggerBindingRef) []slog.Attr {
	return []slog.Attr{
		slog.String("connector", binding.Trigger.ConnectorID), slog.String("connection", binding.Connection.Name),
		slog.String("trigger", binding.Trigger.TriggerName), slog.String("binding", binding.Name),
	}
}
