// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

/**
 * Maps one provider event to a Flow start input or an application RPC input.
 *
 * <p>Keep it deterministic: durable delivery may map the same event again after a restart.
 *
 * @param <P> the payload type
 * @param <I> the input type
 */
public interface TriggerInputMapper<P, I> {
    /**
     * Maps the event.
     *
     * @param event the provider event
     * @return the input
     */
    I mapToInput(TriggerEvent<P> event);
}
