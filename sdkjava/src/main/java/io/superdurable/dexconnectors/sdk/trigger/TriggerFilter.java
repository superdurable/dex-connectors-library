// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

/**
 * Decides whether an application accepts an event; {@code false} consumes it without routing.
 *
 * <p>It must be deterministic and side-effect free, because durable delivery may evaluate it again
 * after a restart.
 *
 * @param <P> the payload type
 */
public interface TriggerFilter<P> {
    /**
     * Decides whether the event is accepted.
     *
     * @param event the provider event
     * @return {@code true} to route the event
     */
    boolean acceptsEvent(TriggerEvent<P> event);
}
