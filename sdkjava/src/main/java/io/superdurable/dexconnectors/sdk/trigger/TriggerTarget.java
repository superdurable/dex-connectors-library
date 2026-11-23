// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

/**
 * Handles one event after the provider transport acknowledges receipt.
 *
 * <p>{@link #handleTrigger} returns when it consumes the event, throws
 * {@link UndeliverableTriggerException} when no retry can deliver it, or throws another exception
 * to keep the event pending for a retry.
 *
 * @param <P> the payload type
 */
public interface TriggerTarget<P> {
    /**
     * Consumes, retries, or marks one provider event undeliverable.
     *
     * @param context the delivery attempt context
     * @param event the provider event
     * @throws Exception to keep the event pending for a retry
     */
    void handleTrigger(TriggerContext context, TriggerEvent<P> event) throws Exception;
}
