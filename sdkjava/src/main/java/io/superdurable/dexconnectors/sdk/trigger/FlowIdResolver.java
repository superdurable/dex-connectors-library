// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.trigger;

/**
 * Maps one provider event to the stable application Flow ID that owns it.
 *
 * @param <P> the payload type
 */
public interface FlowIdResolver<P> {
    /**
     * Resolves the Flow ID.
     *
     * @param event the provider event
     * @return the Flow ID
     */
    String resolveFlowId(TriggerEvent<P> event);
}
