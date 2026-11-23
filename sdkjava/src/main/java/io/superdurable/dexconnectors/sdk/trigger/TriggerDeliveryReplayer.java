// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

/**
 * A target that redelivers events persisted before a previous process stopped.
 *
 * <p>A durable inbox target implements both this interface and {@link TriggerDeliveryPreparer};
 * {@link Triggers#newTrigger} replays it before running the source.
 */
public interface TriggerDeliveryReplayer {
    /**
     * Delivers every pending event in order, returning once all are consumed.
     *
     * @param context the delivery context
     * @throws Exception when replay cannot complete
     */
    void replayTriggerDeliveries(TriggerContext context) throws Exception;
}
