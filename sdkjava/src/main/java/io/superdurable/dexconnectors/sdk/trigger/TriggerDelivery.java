// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

import io.grpc.StatusRuntimeException;
import io.superdurable.dex.exceptions.DexServiceException;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.regex.Pattern;

/**
 * Trigger delivery outcomes, records, and the retrying delivery loop.
 */
public final class TriggerDelivery {
    private static final int MAXIMUM_CAUSE_DEPTH = 32;
    private static final Pattern ERROR_TYPE_NAME = Pattern.compile("^[A-Za-z_][A-Za-z0-9_]{0,63}$");
    private static final Duration RETRY_INITIAL_DELAY = Duration.ofMillis(250);
    private static final Duration RETRY_MAXIMUM_DELAY = Duration.ofSeconds(30);
    private static final TriggerLogger DEFAULT_LOGGER = new StandardTriggerLogger();

    private TriggerDelivery() {
    }

    /**
     * Returns the error when it is already the marker, otherwise a new marker caused by it.
     *
     * @param error the failure
     * @return the undeliverable marker
     */
    public static UndeliverableTriggerException markTriggerUndeliverable(final Throwable error) {
        return error instanceof UndeliverableTriggerException
                ? (UndeliverableTriggerException) error
                : new UndeliverableTriggerException(error);
    }

    /**
     * Reports whether the error or an error in its cause chain, up to 32 links, is the marker.
     *
     * @param error the failure, or {@code null}
     * @return {@code true} for an undeliverable event
     */
    public static boolean isTriggerUndeliverable(final Throwable error) {
        return hasCause(error, UndeliverableTriggerException.class);
    }

    /**
     * Reports whether the error or an error in its cause chain is a delivery abort.
     *
     * @param error the failure, or {@code null}
     * @return {@code true} when cancellation ended delivery
     */
    public static boolean isTriggerDeliveryAborted(final Throwable error) {
        return hasCause(error, TriggerDeliveryAbortedException.class);
    }

    /**
     * Returns the default logger, a {@link StandardTriggerLogger}.
     *
     * @return the default logger
     */
    public static TriggerLogger defaultTriggerLogger() {
        return DEFAULT_LOGGER;
    }

    /**
     * Returns a logger that adds attributes to every record; a record keeps its own value for a
     * shared key.
     *
     * @param logger the base logger, or {@code null} for the default
     * @param attributes the attributes to add
     * @return the binding logger
     */
    public static TriggerLogger bindTriggerLogAttributes(final TriggerLogger logger, final Map<String, Object> attributes) {
        final TriggerLogger base = logger == null ? DEFAULT_LOGGER : logger;
        final Map<String, Object> bound = new LinkedHashMap<>(attributes);
        return new TriggerLogger() {
            @Override
            public boolean isEnabledFor(final TriggerLogLevel level) {
                return base.isEnabledFor(level);
            }

            @Override
            public void log(final TriggerLogLevel level, final String message, final Map<String, Object> recordAttributes) {
                final Map<String, Object> merged = new LinkedHashMap<>(bound);
                merged.putAll(recordAttributes);
                base.log(level, message, merged);
            }
        };
    }

    /**
     * Starts one delivery attempt for a source that retries on its own schedule.
     *
     * <p>The attempt shares the context's cancellation and attributes; its skip recording also
     * reaches the context.
     *
     * @param context the delivery context
     * @return the attempt
     */
    public static TriggerAttempt startTriggerAttempt(final TriggerContext context) {
        final AtomicBoolean isSkipped = new AtomicBoolean();
        final TriggerContext attemptContext = new TriggerContext(context.getCancellation(), context.getLogAttributes(),
                () -> {
                    isSkipped.set(true);
                    context.recordSkip();
                });
        return new TriggerAttempt(attemptContext, isSkipped);
    }

    /**
     * Logs one record with the context's attributes; record attributes win and logger errors are
     * ignored.
     *
     * @param logger the logger, or {@code null} for the default
     * @param context the delivery context
     * @param level the record level
     * @param message the fixed record message
     * @param attributes the safe record attributes, or {@code null}
     */
    public static void logTriggerRecord(
            final TriggerLogger logger, final TriggerContext context, final TriggerLogLevel level,
            final String message, final Map<String, Object> attributes) {
        final TriggerLogger target = logger == null ? DEFAULT_LOGGER : logger;
        try {
            if (!target.isEnabledFor(level)) {
                return;
            }
            final Map<String, Object> merged = new LinkedHashMap<>(context.getLogAttributes());
            if (attributes != null) {
                merged.putAll(attributes);
            }
            target.log(level, message, merged);
        } catch (RuntimeException ignored) {
            // A logger failure cannot change Trigger delivery state.
        }
    }

    /**
     * Returns record attributes for an error: its class, its cause's class, and a gRPC code.
     *
     * <p>Messages are never included, because handlers, providers, and credential stores may put
     * secrets or payload text in them.
     *
     * @param error the failure
     * @return the safe attributes
     */
    public static Map<String, Object> triggerErrorAttributes(final Throwable error) {
        final Map<String, Object> attributes = new LinkedHashMap<>();
        attributes.put("error_type", errorTypeName(error));
        if (error.getCause() != null) {
            attributes.put("error_cause_type", errorTypeName(error.getCause()));
        }
        if (error instanceof DexServiceException) {
            attributes.put("error_code", ((DexServiceException) error).getCode().value());
        } else if (error instanceof StatusRuntimeException) {
            attributes.put("error_code", ((StatusRuntimeException) error).getStatus().getCode().value());
        }
        return attributes;
    }

    /**
     * Returns the delay before retry attempt {@code attempt}: 250 milliseconds doubling to 30 seconds.
     *
     * @param attempt the one-based number of the failed attempt
     * @return the delay
     */
    public static Duration triggerRetryDelay(final int attempt) {
        Duration delay = RETRY_INITIAL_DELAY;
        int completed = 1;
        while (completed < attempt && delay.compareTo(RETRY_MAXIMUM_DELAY) < 0) {
            delay = delay.multipliedBy(2);
            completed++;
        }
        return delay.compareTo(RETRY_MAXIMUM_DELAY) > 0 ? RETRY_MAXIMUM_DELAY : delay;
    }

    /**
     * Waits for a delay unless the cancellation fires first.
     *
     * @param cancellation the cancellation
     * @param delay the delay
     * @throws TriggerDeliveryAbortedException when cancellation fires before or during the wait
     * @throws InterruptedException if the waiting thread is interrupted
     */
    public static void waitForTriggerDelay(final TriggerCancellation cancellation, final Duration delay)
            throws InterruptedException {
        cancellation.throwIfCancelled();
        cancellation.awaitCancellation(delay);
        cancellation.throwIfCancelled();
    }

    /**
     * Asks a durable target to persist an event before acknowledgement; other targets need nothing.
     *
     * @param context the delivery context
     * @param target the target
     * @param event the provider event
     * @param <P> the payload type
     * @throws Exception when the durable target could not persist the event
     */
    @SuppressWarnings("unchecked")
    public static <P> void prepareTriggerDelivery(
            final TriggerContext context, final TriggerTarget<P> target, final TriggerEvent<P> event) throws Exception {
        if (target instanceof TriggerDeliveryPreparer) {
            ((TriggerDeliveryPreparer<P>) target).prepareTrigger(context, event);
        }
    }

    /**
     * Reports whether a target both records and replays events, as a durable inbox does.
     *
     * @param target the target
     * @return {@code true} for a durable inbox target
     */
    public static boolean hasDurableTriggerInbox(final Object target) {
        return target instanceof TriggerDeliveryPreparer && target instanceof TriggerDeliveryReplayer;
    }

    /**
     * Hands one acknowledged event to a target until the target consumes it.
     *
     * <p>A returned attempt or an {@link UndeliverableTriggerException} consumes the event. Every
     * other exception is retried after a delay that starts at 250 milliseconds and doubles up to 30
     * seconds. It throws {@link TriggerDeliveryAbortedException} as soon as the context is
     * cancelled, including during a wait; {@link InterruptedException} propagates unchanged.
     *
     * <p>It logs WARN "trigger delivery failed; retrying" before every wait, INFO "trigger delivered
     * after retry" when a later attempt succeeds, and WARN "trigger event skipped: undeliverable" when
     * the target throws the marker.
     *
     * @param context the delivery context
     * @param target the target
     * @param event the provider event
     * @param logger the logger, or {@code null} for the default
     * @param <P> the payload type
     * @throws InterruptedException if the delivering thread is interrupted
     */
    public static <P> void deliverTrigger(
            final TriggerContext context, final TriggerTarget<P> target, final TriggerEvent<P> event,
            final TriggerLogger logger) throws InterruptedException {
        int attemptNumber = 1;
        while (true) {
            context.getCancellation().throwIfCancelled();
            final TriggerAttempt attempt = startTriggerAttempt(context);
            try {
                target.handleTrigger(attempt.getContext(), event);
            } catch (InterruptedException error) {
                throw error;
            } catch (Exception error) {
                if (isTriggerUndeliverable(error)) {
                    final Map<String, Object> attributes = eventAttributes(event);
                    attributes.putAll(triggerErrorAttributes(error));
                    logTriggerRecord(logger, context, TriggerLogLevel.WARN, "trigger event skipped: undeliverable",
                            attributes);
                    return;
                }
                // The attempt failed because delivery ended, so there is no retry to report.
                context.getCancellation().throwIfCancelled();
                final Duration delay = triggerRetryDelay(attemptNumber);
                final Map<String, Object> attributes = eventAttributes(event);
                attributes.put("attempt", attemptNumber);
                attributes.put("delay_ms", delay.toMillis());
                attributes.putAll(triggerErrorAttributes(error));
                logTriggerRecord(logger, context, TriggerLogLevel.WARN, "trigger delivery failed; retrying", attributes);
                waitForTriggerDelay(context.getCancellation(), delay);
                attemptNumber++;
                continue;
            }
            if (attemptNumber > 1 && !attempt.wasSkipped()) {
                final Map<String, Object> attributes = eventAttributes(event);
                attributes.put("attempts", attemptNumber);
                logTriggerRecord(logger, context, TriggerLogLevel.INFO, "trigger delivered after retry", attributes);
            }
            return;
        }
    }

    static Map<String, Object> eventAttributes(final TriggerEvent<?> event) {
        final Map<String, Object> attributes = new LinkedHashMap<>();
        attributes.put("event_id", event.getId());
        return attributes;
    }

    private static boolean hasCause(final Throwable error, final Class<? extends Throwable> type) {
        Throwable current = error;
        for (int depth = 0; depth < MAXIMUM_CAUSE_DEPTH && current != null; depth++) {
            if (type.isInstance(current)) {
                return true;
            }
            current = current.getCause();
        }
        return false;
    }

    private static String errorTypeName(final Throwable error) {
        final String name = error.getClass().getSimpleName();
        return ERROR_TYPE_NAME.matcher(name).matches() ? name : "Error";
    }
}
