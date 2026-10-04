// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/triggerlog"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// DurableTriggerOption configures NewDurableTriggerTarget.
type DurableTriggerOption interface {
	applyDurableTriggerOption(*durableTriggerOptions)
}

type durableTriggerOptions struct {
	logger *slog.Logger
}

type durableTriggerLoggerOption struct{ logger *slog.Logger }

func (option durableTriggerLoggerOption) applyDurableTriggerOption(options *durableTriggerOptions) {
	options.logger = option.logger
}

// WithTriggerLogger sends the inbox's records to logger. Without this option, or with a nil logger,
// records go to slog.Default() as of each record. Every record carries the connector, connection,
// trigger, and binding attributes, and event IDs rather than payloads.
func WithTriggerLogger(logger *slog.Logger) DurableTriggerOption {
	return durableTriggerLoggerOption{logger: logger}
}

type durableTriggerTarget[T any] struct {
	inbox  *projectconfig.TriggerInbox
	target sdkgo.TriggerTarget[T]
	log    triggerlog.Logger
	mutex  sync.Mutex
	// pendingRemoval holds the IDs of events the target consumed whose removal from the inbox failed, and
	// whether the target consumed each as undeliverable. A retry of such an event only removes it, so a
	// failing inbox write does not invoke the target again.
	pendingRemoval map[string]bool
}

// NewDurableTriggerTarget stores acknowledged events in the project Trigger inbox until the target consumes
// them. The target consumes an event by returning nil or an UndeliverableTriggerError; any other error keeps
// the event pending. Replay delivers pending events in order with sdkgo.DeliverTrigger. Inbox read and write
// failures are returned like target failures, so callers retry them. When only the removal of a consumed
// event fails, a retry removes the event without invoking the target again.
//
// The inbox logs a WARN "trigger event skipped: undeliverable" record for every event it consumes as
// undeliverable, an ERROR record for every inbox read, write, or removal failure, and INFO records at the
// start and end of a replay that finds pending events. Pass WithTriggerLogger to choose the logger.
func NewDurableTriggerTarget[T any](inbox *projectconfig.TriggerInbox, key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[T], options ...DurableTriggerOption) (sdkgo.TriggerTarget[T], error) {
	if inbox == nil || target == nil {
		return nil, errors.New("project trigger inbox and Trigger target are required")
	}
	var resolved durableTriggerOptions
	for _, option := range options {
		if option != nil {
			option.applyDurableTriggerOption(&resolved)
		}
	}
	log := triggerlog.New(resolved.logger,
		slog.String("connector", key.ConnectorID), slog.String("connection", key.ConnectionName),
		slog.String("trigger", key.TriggerName), slog.String("binding", key.BindingName),
	)
	return &durableTriggerTarget[T]{inbox: inbox, target: target, log: log, pendingRemoval: make(map[string]bool)}, nil
}

// PrepareTrigger stores the event before its source acknowledges it.
func (target *durableTriggerTarget[T]) PrepareTrigger(ctx context.Context, event sdkgo.TriggerEvent[T]) error {
	if strings.TrimSpace(event.ID) == "" {
		return errors.New("Trigger event ID is required")
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode Trigger event: %w", err)
	}
	target.mutex.Lock()
	defer target.mutex.Unlock()
	if err := target.inbox.Add(ctx, projectconfig.PendingTriggerEvent{ID: event.ID, Event: encoded}); err != nil {
		target.log.Error(ctx, "trigger inbox write failed", slog.String("event_id", event.ID), triggerlog.Err(err))
		return err
	}
	return nil
}

// HandleTrigger delivers one event and removes it once the target consumes it.
func (target *durableTriggerTarget[T]) HandleTrigger(ctx context.Context, event sdkgo.TriggerEvent[T]) error {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	_, err := target.consume(ctx, event)
	return err
}

// ReplayTriggerDeliveries delivers the events pending at the call in order. It holds the inbox lock only
// for one delivery attempt, so PrepareTrigger does not wait for a pending event's backoff. A source must
// still deliver new events only after replay returns, or a new event could overtake an older pending one.
func (target *durableTriggerTarget[T]) ReplayTriggerDeliveries(ctx context.Context) error {
	pending, err := target.inbox.Pending(ctx)
	if err != nil {
		target.log.Error(ctx, "trigger inbox read failed", triggerlog.Err(err))
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	target.log.Info(ctx, "replaying pending trigger events", slog.Int("count", len(pending)))
	deliveryLogger := sdkgo.WithTriggerLogger(target.log.Slog())
	delivered, skipped := 0, 0
	for index, stored := range pending {
		var event sdkgo.TriggerEvent[T]
		if err := json.Unmarshal(stored.Event, &event); err != nil {
			target.log.Error(ctx, "trigger inbox event decode failed", slog.String("event_id", stored.ID), triggerlog.Err(err))
			return fmt.Errorf("decode pending Trigger event: %w", err)
		}
		eventSkipped := false
		err := sdkgo.DeliverTrigger(ctx, sdkgo.TriggerTargetFunc[T](func(ctx context.Context, event sdkgo.TriggerEvent[T]) error {
			var err error
			eventSkipped, err = target.handlePending(ctx, event)
			return err
		}), event, deliveryLogger)
		if err != nil {
			target.log.Info(ctx, "finished replaying pending trigger events", slog.Int("delivered", delivered),
				slog.Int("skipped", skipped), slog.Int("remaining", len(pending)-index), triggerlog.Err(err))
			return err
		}
		if eventSkipped {
			skipped++
		} else {
			delivered++
		}
	}
	target.log.Info(ctx, "finished replaying pending trigger events",
		slog.Int("delivered", delivered), slog.Int("skipped", skipped), slog.Int("remaining", 0))
	return nil
}

// handlePending makes one replay attempt, skipping an event that another delivery already consumed. It
// reports whether the target consumed the event as undeliverable.
func (target *durableTriggerTarget[T]) handlePending(ctx context.Context, event sdkgo.TriggerEvent[T]) (bool, error) {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	pending, err := target.inbox.Pending(ctx)
	if err != nil {
		target.log.Error(ctx, "trigger inbox read failed", slog.String("event_id", event.ID), triggerlog.Err(err))
		return false, err
	}
	for _, stored := range pending {
		if stored.ID == event.ID {
			return target.consume(ctx, event)
		}
	}
	return false, nil
}

// consume delivers one event and removes it when the target consumes it. It reports whether the target
// consumed the event as undeliverable, and logs that skip once. The caller holds target.mutex.
func (target *durableTriggerTarget[T]) consume(ctx context.Context, event sdkgo.TriggerEvent[T]) (bool, error) {
	skipped, removalPending := target.pendingRemoval[event.ID]
	if !removalPending {
		err := target.target.HandleTrigger(ctx, event)
		if err != nil && !sdkgo.IsTriggerUndeliverable(err) {
			return false, err
		}
		if err != nil {
			skipped = true
			target.log.Warn(ctx, "trigger event skipped: undeliverable",
				append([]slog.Attr{slog.String("event_id", event.ID)}, triggerlog.ErrAttrs(err)...)...)
		}
	}
	if skipped {
		// The enclosing delivery attempt must not also report this event as delivered.
		triggerlog.RecordSkip(ctx)
	}
	if err := target.inbox.Remove(ctx, event.ID); err != nil {
		target.pendingRemoval[event.ID] = skipped
		target.log.Error(ctx, "trigger inbox remove failed", slog.String("event_id", event.ID), triggerlog.Err(err))
		return skipped, err
	}
	delete(target.pendingRemoval, event.ID)
	return skipped, nil
}
