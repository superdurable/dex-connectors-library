// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.projectconfig;

import io.superdurable.dexconnectors.sdk.trigger.TriggerContext;
import io.superdurable.dexconnectors.sdk.trigger.TriggerDelivery;
import io.superdurable.dexconnectors.sdk.trigger.TriggerDeliveryPreparer;
import io.superdurable.dexconnectors.sdk.trigger.TriggerDeliveryReplayer;
import io.superdurable.dexconnectors.sdk.trigger.TriggerEvent;
import io.superdurable.dexconnectors.sdk.trigger.TriggerLogLevel;
import io.superdurable.dexconnectors.sdk.trigger.TriggerLogger;
import io.superdurable.dexconnectors.sdk.trigger.TriggerTarget;
import java.util.HashMap;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Stores acknowledged events in a project {@link TriggerInbox} until a target consumes them.
 *
 * <p>The target consumes an event by returning or by throwing
 * {@link io.superdurable.dexconnectors.sdk.trigger.UndeliverableTriggerException}; any other
 * exception keeps it pending. {@link #replayTriggerDeliveries} delivers pending events in order
 * with {@link TriggerDelivery#deliverTrigger}, and a source delivers new events only after replay
 * returns. Inbox failures throw like target failures, so callers retry them; when only a removal
 * failed, a retry removes the event without calling the target again. {@link #prepareTrigger}
 * refuses a write once the context is cancelled. Records carry the binding identity. It logs WARN
 * for undeliverable skips, ERROR for inbox failures, and INFO at the start and end of a replay that
 * finds pending events.
 *
 * @param <P> the payload type
 */
public final class DurableTriggerTarget<P>
        implements TriggerTarget<P>, TriggerDeliveryPreparer<P>, TriggerDeliveryReplayer {
    private final TriggerInbox inbox;
    private final TriggerTarget<P> target;
    private final TriggerLogger logger;
    private final TriggerEventCodec.PayloadDecoder<P> decodePayload;
    private final TriggerEventCodec.PayloadEncoder<P> encodePayload;
    private final Object lock = new Object();
    private final Map<String, Boolean> pendingRemovalByEventId = new HashMap<>();

    /**
     * Wraps a target with the project Trigger inbox.
     *
     * @param inbox the binding's inbox
     * @param target the consuming target
     * @param decodePayload decodes replayed payloads, such as {@link TriggerEventCodec#jsonPayloadDecoder()}
     * @param encodePayload maps a payload to its stored JSON value, or {@code null} for Jackson
     * @param logger the record destination, or {@code null} for the default
     * @throws IllegalArgumentException if the inbox, target, or decoder is missing
     */
    public DurableTriggerTarget(
            final TriggerInbox inbox, final TriggerTarget<P> target,
            final TriggerEventCodec.PayloadDecoder<P> decodePayload,
            final TriggerEventCodec.PayloadEncoder<P> encodePayload, final TriggerLogger logger) {
        if (inbox == null || target == null || decodePayload == null) {
            throw new IllegalArgumentException("project trigger inbox, Trigger target, and payload decoder are required");
        }
        final TriggerInboxKey identity = inbox.getIdentity();
        final Map<String, Object> attributes = new LinkedHashMap<>();
        attributes.put("connector", identity.getConnectorId());
        attributes.put("connection", identity.getConnectionName());
        attributes.put("trigger", identity.getTriggerName());
        attributes.put("binding", identity.getBindingName());
        this.inbox = inbox;
        this.target = target;
        this.logger = TriggerDelivery.bindTriggerLogAttributes(logger, attributes);
        this.decodePayload = decodePayload;
        this.encodePayload = encodePayload;
    }

    @Override
    public void prepareTrigger(final TriggerContext context, final TriggerEvent<P> event) throws Exception {
        if (PollCursorNames.isGoBlank(event.getId())) {
            throw new IllegalArgumentException("Trigger event ID is required");
        }
        final byte[] encoded = TriggerEventCodec.encodeTriggerEvent(event, encodePayload);
        synchronized (lock) {
            // A source that abandoned this acknowledgement, such as a timed-out webhook, must not record late.
            context.getCancellation().throwIfCancelled();
            try {
                inbox.add(new PendingTriggerEvent(event.getId(), encoded));
            } catch (Exception error) {
                log(context, TriggerLogLevel.ERROR, "trigger inbox write failed", event.getId(), error);
                throw error;
            }
        }
    }

    @Override
    public void handleTrigger(final TriggerContext context, final TriggerEvent<P> event) throws Exception {
        synchronized (lock) {
            consume(context, event);
        }
    }

    @Override
    public void replayTriggerDeliveries(final TriggerContext context) throws Exception {
        final List<PendingTriggerEvent> pending;
        try {
            pending = inbox.pending();
        } catch (Exception error) {
            log(context, TriggerLogLevel.ERROR, "trigger inbox read failed", null, error);
            throw error;
        }
        if (pending.isEmpty()) {
            return;
        }
        final Map<String, Object> start = new LinkedHashMap<>();
        start.put("count", pending.size());
        TriggerDelivery.logTriggerRecord(logger, context, TriggerLogLevel.INFO, "replaying pending trigger events", start);
        int delivered = 0;
        int skipped = 0;
        for (int index = 0; index < pending.size(); index++) {
            final PendingTriggerEvent stored = pending.get(index);
            final TriggerEvent<P> event;
            try {
                event = TriggerEventCodec.decodeTriggerEvent(stored.eventBytes(), decodePayload);
            } catch (Exception error) {
                log(context, TriggerLogLevel.ERROR, "trigger inbox event decode failed", stored.getId(), error);
                throw new IllegalStateException("decode pending Trigger event", error);
            }
            final PendingReplayAttempt attempt = new PendingReplayAttempt();
            try {
                TriggerDelivery.deliverTrigger(context, attempt, event, logger);
            } catch (Exception error) {
                final Map<String, Object> finished = finishedAttributes(delivered, skipped, pending.size() - index);
                finished.putAll(TriggerDelivery.triggerErrorAttributes(error));
                TriggerDelivery.logTriggerRecord(logger, context, TriggerLogLevel.INFO,
                        "finished replaying pending trigger events", finished);
                throw error;
            }
            if (attempt.isSkipped) {
                skipped++;
            } else {
                delivered++;
            }
        }
        TriggerDelivery.logTriggerRecord(logger, context, TriggerLogLevel.INFO, "finished replaying pending trigger events",
                finishedAttributes(delivered, skipped, 0));
    }

    private boolean handlePending(final TriggerContext context, final TriggerEvent<P> event) throws Exception {
        final List<PendingTriggerEvent> pending;
        try {
            pending = inbox.pending();
        } catch (Exception error) {
            log(context, TriggerLogLevel.ERROR, "trigger inbox read failed", event.getId(), error);
            throw error;
        }
        for (PendingTriggerEvent stored : pending) {
            if (stored.getId().equals(event.getId())) {
                return consume(context, event);
            }
        }
        return false;
    }

    private boolean consume(final TriggerContext context, final TriggerEvent<P> event) throws Exception {
        Boolean isSkipped = pendingRemovalByEventId.get(event.getId());
        if (isSkipped == null) {
            isSkipped = false;
            try {
                target.handleTrigger(context, event);
            } catch (Exception error) {
                if (!TriggerDelivery.isTriggerUndeliverable(error)) {
                    throw error;
                }
                isSkipped = true;
                log(context, TriggerLogLevel.WARN, "trigger event skipped: undeliverable", event.getId(), error);
            }
        }
        // The enclosing delivery attempt must not also report this event as delivered.
        if (isSkipped) {
            context.recordSkip();
        }
        try {
            inbox.remove(event.getId());
        } catch (Exception error) {
            pendingRemovalByEventId.put(event.getId(), isSkipped);
            log(context, TriggerLogLevel.ERROR, "trigger inbox remove failed", event.getId(), error);
            throw error;
        }
        pendingRemovalByEventId.remove(event.getId());
        return isSkipped;
    }

    private void log(
            final TriggerContext context, final TriggerLogLevel level, final String message, final String eventId,
            final Throwable error) {
        final Map<String, Object> attributes = new LinkedHashMap<>();
        if (eventId != null) {
            attributes.put("event_id", eventId);
        }
        attributes.putAll(TriggerDelivery.triggerErrorAttributes(error));
        TriggerDelivery.logTriggerRecord(logger, context, level, message, attributes);
    }

    private static Map<String, Object> finishedAttributes(final int delivered, final int skipped, final int remaining) {
        final Map<String, Object> attributes = new LinkedHashMap<>();
        attributes.put("delivered", delivered);
        attributes.put("skipped", skipped);
        attributes.put("remaining", remaining);
        return attributes;
    }

    private final class PendingReplayAttempt implements TriggerTarget<P> {
        private boolean isSkipped;

        @Override
        public void handleTrigger(final TriggerContext context, final TriggerEvent<P> event) throws Exception {
            // The lock covers one attempt, so prepareTrigger never waits for a pending event's backoff.
            synchronized (lock) {
                isSkipped = handlePending(context, event);
            }
        }
    }
}
