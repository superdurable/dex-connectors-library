// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

/**
 * Owns the provider transport and emits acknowledged events.
 *
 * <p>{@link #run} calls {@link TriggerDelivery#prepareTriggerDelivery} before acknowledging an
 * event, retries a retryable error, and treats {@link UndeliverableTriggerException} as consumed.
 * It returns or throws only when the context is cancelled or it cannot recover.
 *
 * @param <P> the payload type
 */
public interface TriggerSource<P> {
    /**
     * Processes provider events until the context is cancelled or an unrecoverable error.
     *
     * @param context the run context
     * @param target the application target
     * @throws Exception when the source stops; {@link TriggerDeliveryAbortedException} after cancellation
     */
    void run(TriggerContext context, TriggerTarget<P> target) throws Exception;
}
