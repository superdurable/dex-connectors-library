// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

/**
 * A target that persists an event before the provider acknowledges it, such as a durable inbox.
 *
 * @param <P> the payload type
 */
public interface TriggerDeliveryPreparer<P> {
    /**
     * Persists an event before provider acknowledgement.
     *
     * @param context the delivery context
     * @param event the provider event
     * @throws Exception when the event could not be persisted; the source must not acknowledge it
     */
    void prepareTrigger(TriggerContext context, TriggerEvent<P> event) throws Exception;
}
